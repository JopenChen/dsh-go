// 本文件实现本地后端多探测链 + LocalProvider。
//
// 对齐官方：packages/sandbox/sandbox-local/src/index.ts
//
// 设计要点：
//   - 平台检测：Linux 链 bwrap → landlock，macOS 链 seatbelt，Windows 链 windows-acl；
//   - 功能探测：用 read-only profile + true 命令实际运行，成功则后端可用；
//   - 探测结果缓存：进程级单例，只探测一次；
//   - LocalProvider 实现 SandboxProvider 接口：Confine() 根据后端类型组装完整 argv；
//   - 后端特定 denialSignatures：bwrap EROFS、landlock EACCES、seatbelt EPERM；
//   - Config 支持 runnerCommand 覆盖（跳过探测，直接使用指定 runner）。
package sandbox

import (
	"os/exec"
	"runtime"
	"sync"
)

// BackendType 是本地沙箱后端类型。
type BackendType string

const (
	// BackendBwrap 是 Linux Bubblewrap 后端。
	BackendBwrap BackendType = "bwrap"
	// BackendLandlock 是 Linux Landlock 后端。
	BackendLandlock BackendType = "landlock"
	// BackendSeatbelt 是 macOS Seatbelt 后端。
	BackendSeatbelt BackendType = "seatbelt"
	// BackendWindowsACL 是 Windows ACL 后端。
	BackendWindowsACL BackendType = "windows-acl"
	// BackendUnavailable 表示没有可用的本地后端。
	BackendUnavailable BackendType = "unavailable"
)

// LocalConfig 是 LocalProvider 的配置。
type LocalConfig struct {
	// RunnerCommand 覆盖自动探测的 runner 命令（如 "bwrap"、"sandbox-exec"）。
	// 非空时跳过探测，直接使用此命令。
	RunnerCommand string
	// BackendType 强制指定后端类型（配合 RunnerCommand 使用）。
	BackendType BackendType
}

// LocalProvider 是本地沙箱提供者，实现 SandboxProvider 接口。
// 通过多后端探测链选择当前平台可用的最强后端。
type LocalProvider struct {
	config LocalConfig
	once   sync.Once
	backend BackendType
	runner  string
}

// NewLocalProvider 创建本地沙箱提供者。
// config 为 nil 时使用默认配置（自动探测）。
func NewLocalProvider(config *LocalConfig) *LocalProvider {
	p := &LocalProvider{}
	if config != nil {
		p.config = *config
	}
	return p
}

// detectBackend 执行多后端探测链，返回可用的后端类型和 runner 命令。
// 探测结果缓存（进程级单例）。
func (p *LocalProvider) detectBackend() (BackendType, string) {
	p.once.Do(func() {
		// 如果配置了强制后端，直接使用
		if p.config.RunnerCommand != "" && p.config.BackendType != "" {
			p.backend = p.config.BackendType
			p.runner = p.config.RunnerCommand
			return
		}
		// 按平台探测
		switch runtime.GOOS {
		case "linux":
			// Linux 链：bwrap → landlock
			if p.tryBwrap() {
				p.backend = BackendBwrap
				p.runner = "bwrap"
				return
			}
			if p.tryLandlock() {
				p.backend = BackendLandlock
				p.runner = "landlock-launcher"
				return
			}
		case "darwin":
			// macOS 链：seatbelt
			if p.trySeatbelt() {
				p.backend = BackendSeatbelt
				p.runner = "sandbox-exec"
				return
			}
		case "windows":
			// Windows 链：windows-acl（通过受限令牌实现）
			p.backend = BackendWindowsACL
			p.runner = "windows-acl"
			return
		}
		p.backend = BackendUnavailable
		p.runner = ""
	})
	return p.backend, p.runner
}

// tryBwrap 探测 bwrap 是否可用：用 read-only profile 运行 true 命令。
func (p *LocalProvider) tryBwrap() bool {
	_, err := exec.LookPath("bwrap")
	if err != nil {
		return false
	}
	// 用 read-only profile 运行 true，成功则可用
	cmd := exec.Command("bwrap",
		"--ro-bind", "/", "/",
		"--dev", "/dev",
		"--unshare-pid",
		"--proc", "/proc",
		"--die-with-parent",
		"--", "true",
	)
	return cmd.Run() == nil
}

// tryLandlock 探测 landlock-launcher 是否可用。
func (p *LocalProvider) tryLandlock() bool {
	_, err := exec.LookPath("landlock-launcher")
	return err == nil
}

// trySeatbelt 探测 macOS sandbox-exec 是否可用。
func (p *LocalProvider) trySeatbelt() bool {
	_, err := exec.LookPath("sandbox-exec")
	if err != nil {
		return false
	}
	// 用基础 SBPL 运行 true
	cmd := exec.Command("sandbox-exec", "-p",
		"(version 1)(allow default)(deny file-write*)(allow file-write* (literal \"/dev/null\"))",
		"true",
	)
	return cmd.Run() == nil
}

// Backend 返回当前探测到的后端类型。
func (p *LocalProvider) Backend() BackendType {
	b, _ := p.detectBackend()
	return b
}

// Runner 返回当前探测到的 runner 命令。
func (p *LocalProvider) Runner() string {
	_, r := p.detectBackend()
	return r
}

// Available 检查本地沙箱是否可用。
func (p *LocalProvider) Available() bool {
	b, _ := p.detectBackend()
	return b != BackendUnavailable
}

// Confine 实现 SandboxProvider 接口：根据后端类型组装受约束的 argv。
func (p *LocalProvider) Confine(argv []string, policy SandboxPolicy) (ConfinedArgv, error) {
	backend, runner := p.detectBackend()
	if backend == BackendUnavailable {
		return ConfinedArgv{}, &SandboxUnavailableError{Msg: "no local sandbox backend available on this platform"}
	}
	// Windows ACL 后端不通过 runner 命令，而是通过受限令牌（在 windows_acl.go 中处理）
	if backend == BackendWindowsACL {
		// 对于 Windows ACL，argv 保持不变，enforcement 为 full
		// 实际的令牌创建在执行层处理
		return ConfinedArgv{
			Argv:        argv,
			Enforcement: EnforcementFull,
			DenialSignatures: p.DenialSignatures(),
		}, nil
	}
	// 组装 profile 参数
	var profileArgs []string
	switch backend {
	case BackendBwrap:
		profileArgs = BwrapProfileArgs(policy)
	case BackendLandlock:
		profileArgs = LandlockProfileArgs(policy)
	case BackendSeatbelt:
		profileArgs = SeatbeltProfileArgs(policy)
	}
	// 完整 argv：runner + profileArgs + -- + argv
	fullArgv := []string{runner}
	fullArgv = append(fullArgv, profileArgs...)
	fullArgv = append(fullArgv, "--")
	fullArgv = append(fullArgv, argv...)
	return ConfinedArgv{
		Argv:        fullArgv,
		Enforcement: EnforcementFull,
		DenialSignatures: p.DenialSignatures(),
	}, nil
}

// DenialSignatures 返回当前后端的拒绝签名（用于 RunnerFailureRule 匹配）。
func (p *LocalProvider) DenialSignatures() []string {
	backend, _ := p.detectBackend()
	switch backend {
	case BackendBwrap:
		return []string{"Read-only file system", "EROFS"}
	case BackendLandlock:
		return []string{"Permission denied", "EACCES"}
	case BackendSeatbelt:
		return []string{"Operation not permitted", "EPERM"}
	case BackendWindowsACL:
		return []string{"Access is denied", "ERROR_ACCESS_DENIED"}
	default:
		return nil
	}
}
