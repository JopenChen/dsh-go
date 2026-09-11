// 本文件复刻官方 subagent/src/depth.ts：委托深度核算。顶层代理深度为 0，每派生
// 一层子代理深度 +1；持久化头与运行期选项取 max（运行期只能加深、不能降低，否则
// 一个恢复出来的子代理会被当成顶层而获得过多委托预算）。
package subagent

import "errors"

// ErrInvalidDepth 表示深度/上限不是非负整数。
var ErrInvalidDepth = errors.New("subagent: depth must be a non-negative integer")

// ChildDepth 返回父深度派生一个子代理时的深度（+1）。
func ChildDepth(parentDepth int) (int, error) {
	if parentDepth < 0 {
		return 0, ErrInvalidDepth
	}
	return parentDepth + 1, nil
}

// ResolveDepth 合并持久化头深度与运行期深度，取较大者（运行期只可加深）。
func ResolveDepth(headerDepth, runtimeDepth int) (int, error) {
	if headerDepth < 0 || runtimeDepth < 0 {
		return 0, ErrInvalidDepth
	}
	if runtimeDepth > headerDepth {
		return runtimeDepth, nil
	}
	return headerDepth, nil
}

// CanDelegate 判断当前深度是否还能再派生子代理。
// maxDepth < 0 表示不限制；否则 depth 必须小于 maxDepth。
func CanDelegate(depth, maxDepth int) bool {
	if depth < 0 {
		return false
	}
	if maxDepth < 0 {
		return true
	}
	return depth < maxDepth
}
