// 本文件对应任务 M30：Invariant Registry 不变量校验。
package tests

import (
	"errors"
	"strings"
	"testing"

	"github.com/JopenChen/dsh-go/pkg/invariant"
)

// TestInvariantPkgAttribution 验证违规错误的包归属前缀与明细。
func TestInvariantPkgAttribution(t *testing.T) {
	reg := invariant.NewRegistry()

	// 注册一个必然违规的检查器（模拟 turn 已开再次 turn/start）
	err := reg.Register("pkg/session", func() error {
		return &invariant.InvariantError{Pkg: "pkg/session", Msg: "turn/start while turn already open"}
	})
	if err != nil {
		t.Fatalf("Register 失败: %v", err)
	}

	failures := reg.Run()
	if len(failures) != 1 {
		t.Fatalf("Run() 应返回 1 个失败, 实际 %d", len(failures))
	}
	if !strings.HasPrefix(failures[0].Error(), "INVARIANT [pkg/session]: ") {
		t.Fatalf("错误前缀不匹配: %q", failures[0].Error())
	}

	// 账本登记
	ledger := reg.Ledger()
	if len(ledger) != 1 {
		t.Fatalf("Ledger 应有 1 条记录, 实际 %d", len(ledger))
	}
}

// TestInvariantMultiplePkgsOrdered 验证多包注册时按包名排序输出，且各包独立。
func TestInvariantMultiplePkgsOrdered(t *testing.T) {
	reg := invariant.NewRegistry()

	// 包 B 先注册，包 A 后注册；Run 应按包名字典序（A 在前）输出
	_ = reg.Register("pkg/zeta", func() error { return errors.New("z bug") })
	_ = reg.Register("pkg/alpha", func() error { return errors.New("a bug") })

	failures := reg.Run()
	if len(failures) != 2 {
		t.Fatalf("Run() 应返回 2 个失败, 实际 %d", len(failures))
	}
	// 非 InvariantError 应被包装为带包名的错误
	if !strings.HasPrefix(failures[0].Error(), "INVARIANT [pkg/alpha]: ") {
		t.Fatalf("第一个失败应归属 pkg/alpha: %q", failures[0].Error())
	}
	if !strings.HasPrefix(failures[1].Error(), "INVARIANT [pkg/zeta]: ") {
		t.Fatalf("第二个失败应归属 pkg/zeta: %q", failures[1].Error())
	}
}

// TestInvariantRegisterValidation 验证非法注册参数被拒绝。
func TestInvariantRegisterValidation(t *testing.T) {
	reg := invariant.NewRegistry()

	if err := reg.Register("", func() error { return nil }); err == nil {
		t.Fatal("空包名注册应报错")
	}
	if err := reg.Register("pkg/x", nil); err == nil {
		t.Fatal("nil 检查器注册应报错")
	}
}

// TestInvariantDisableInProduction 验证关闭开关后 Run 不执行（生产零开销）。
func TestInvariantDisableInProduction(t *testing.T) {
	reg := invariant.NewRegistry()
	_ = reg.Register("pkg/session", func() error {
		return errors.New("should not run when disabled")
	})

	// 记录原始开关状态，测试结束后恢复，避免影响其它用例
	original := invariant.IsEnabled()
	defer invariant.SetEnabled(original)

	invariant.SetEnabled(false)
	if invariant.IsEnabled() {
		t.Fatal("SetEnabled(false) 后 IsEnabled 应为 false")
	}
	if failures := reg.Run(); len(failures) != 0 {
		t.Fatalf("关闭开关后 Run 应返回空, 实际 %d 个失败", len(failures))
	}

	// 重新开启后应恢复校验
	invariant.SetEnabled(true)
	if failures := reg.Run(); len(failures) != 1 {
		t.Fatalf("重新开启后 Run 应返回 1 个失败, 实际 %d", len(failures))
	}
}

// TestInvariantCheckerPanicIsolated 验证单个检查器 panic 不拖垮整体校验。
func TestInvariantCheckerPanicIsolated(t *testing.T) {
	reg := invariant.NewRegistry()
	_ = reg.Register("pkg/panicky", func() error {
		panic("boom")
	})
	_ = reg.Register("pkg/ok", func() error { return nil })

	failures := reg.Run()
	if len(failures) != 1 {
		t.Fatalf("panic 检查器应被捕获为 1 个失败, 实际 %d", len(failures))
	}
	if !strings.Contains(failures[0].Error(), "panicked") {
		t.Fatalf("应提示检查器 panic: %q", failures[0].Error())
	}
}

// TestInvariantMustSatisfy 验证 MustSatisfy 在违规时 panic、通过时不 panic。
func TestInvariantMustSatisfy(t *testing.T) {
	reg := invariant.NewRegistry()

	// 通过场景
	_ = reg.Register("pkg/clean", func() error { return nil })
	func() {
		defer func() {
			if p := recover(); p != nil {
				t.Fatalf("不应 panic: %v", p)
			}
		}()
		reg.MustSatisfy()
	}()

	// 违规场景
	_ = reg.Register("pkg/dirty", func() error { return errors.New("bad state") })
	func() {
		defer func() {
			if p := recover(); p == nil {
				t.Fatal("违规时 MustSatisfy 应 panic")
			}
		}()
		reg.MustSatisfy()
	}()
}
