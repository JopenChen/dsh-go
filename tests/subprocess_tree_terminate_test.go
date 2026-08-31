// Package tests 的子进程接缝（M36）验收测试。
//
// 覆盖：
//   - scrubbedParentEnv：丢弃 DSH_* 环境项（子进程看不到）
//   - CollectedOutput 超过阈值触发 Spill 写入（完整流可从 spill 还原）
//   - 进程树终止（terminate 升级）后 Done 正常结算、进程被回收
//   - 退出事实（exitCode）
package tests

import (
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/JopenChen/dsh-go/pkg/subprocess"
)

// envHelper 构造一个打印 DSH_* 变量是否为空的命令（跨平台）。
func envCheckArgv(varName string) []string {
	if runtime.GOOS == "windows" {
		// if defined 打印状态且始终退出 0。
		return []string{"cmd", "/c", "if defined " + varName + " (echo PRESENT) else (echo EMPTY)"}
	}
	return []string{"sh", "-c", `if [ -z "$` + varName + `" ]; then echo EMPTY; else echo PRESENT; fi`}
}

// TestSubprocessScrubEnv 验证 DSH_* 环境项被 scrub 掉（子进程读不到）。
func TestSubprocessScrubEnv(t *testing.T) {
	t.Setenv("DSH_TEST_FACT", "secret")
	rt := subprocess.NewLocal()
	h := rt.Spawn(subprocess.SubprocessSpawnSpec{
		Argv: envCheckArgv("DSH_TEST_FACT"),
		Cwd:  t.TempDir(),
	})
	out := h.Collected().Stdout()
	res := <-h.Done()
	if res.ExitCode != 0 {
		t.Fatalf("子进程退出码应 0, 实际 %d", res.ExitCode)
	}
	co := out.ReadOutput(0)
	if !strings.Contains(co.Text, "EMPTY") {
		t.Fatalf("DSH_* 被 scrub 后子进程应读到空: %q", co.Text)
	}
}

// TestSubprocessCollectedSpill 验证收集超过阈值 → 截断 + spill 含完整流。
func TestSubprocessCollectedSpill(t *testing.T) {
	var argv []string
	if runtime.GOOS == "windows" {
		argv = []string{"cmd", "/c", "for /L %i in (1,1,3000) do @echo line of output no %i"}
	} else {
		argv = []string{"sh", "-c", "seq 1 3000"}
	}
	rt := subprocess.NewLocal()
	h := rt.Spawn(subprocess.SubprocessSpawnSpec{
		Argv:          argv,
		Cwd:           t.TempDir(),
		StdoutMaxBytes: 1024, // 触发截断
		SpillRoot:      t.TempDir(),
	})
	res := <-h.Done()
	if res.ExitCode != 0 {
		t.Fatalf("退出码应 0, 实际 %d", res.ExitCode)
	}
	co := h.Collected().Stdout().ReadOutput(0)
	if !co.Truncated {
		t.Fatal("输出超过阈值应 truncated")
	}
	if co.SpillPath == "" {
		t.Fatal("启用 spill 时应生成 spill 路径")
	}
	// 从 spill 还原（此处通过文解析器所在进程直接读文件验证完整性）。
	data := readSpill(t, co.SpillPath)
	if len(data) <= 1024 {
		t.Fatalf("spill 应含完整流(>1KB), 实际 %d 字节", len(data))
	}
}

func readSpill(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读 spill 失败: %v", err)
	}
	return b
}

// TestSubprocessTerminate 验证树终止升级后 Done 结算（进程被回收，非正常退出）。
func TestSubprocessTerminate(t *testing.T) {
	var argv []string
	if runtime.GOOS == "windows" {
		argv = []string{"cmd", "/c", "ping -n 60 127.0.0.1 >nul"}
	} else {
		argv = []string{"sh", "-c", "sleep 1000"}
	}
	rt := subprocess.NewLocal()
	h := rt.Spawn(subprocess.SubprocessSpawnSpec{Argv: argv, Cwd: t.TempDir(), GraceMs: 100})
	h.Terminate()
	select {
	case res := <-h.Done():
		// 终止后退出码不应为 0（SIGKILL/强制终止）。
		if res.ExitCode == 0 {
			t.Fatalf("终止后退出码不应为 0, 实际 %+v", res)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("terminate 后 Done 未在超时内结算")
	}
}

// TestSubprocessOutcomeSuccess 验证正常退出给出退出码 0。
func TestSubprocessOutcomeSuccess(t *testing.T) {
	var argv []string
	if runtime.GOOS == "windows" {
		argv = []string{"cmd", "/c", "exit 0"}
	} else {
		argv = []string{"sh", "-c", "exit 0"}
	}
	rt := subprocess.NewLocal()
	h := rt.Spawn(subprocess.SubprocessSpawnSpec{Argv: argv, Cwd: t.TempDir()})
	res := <-h.Done()
	if res.ExitCode != 0 {
		t.Fatalf("正常退出码应 0, 实际 %+v", res)
	}
}