// Package launchenv 复刻官方 util/launch-environment：启动时把多层环境变量
// （进程环境 > 项目 .env > 用户 .env）冻结成不可变快照，按固定信任顺序解析，
// 并在 Windows 上对变量名做大小写折叠。
package launchenv

import "runtime"

// Source 标识值来自哪一层，信任度从高到低。
type Source string

const (
	SourceProcess    Source = "process"
	SourceProjectEnv Source = "project-env"
	SourceUserEnv    Source = "user-env"
)

// sourceOrder 为固定信任顺序。
var sourceOrder = []Source{SourceProcess, SourceProjectEnv, SourceUserEnv}

// Entry 是一次解析命中的条目。
type Entry struct {
	Value  string
	Source Source
	Path   string // 来源文件路径，进程层为空
}

type layer struct {
	path   string
	values map[string]string
}

// Snapshot 是一次启动的不可变环境快照。
type Snapshot struct {
	bySource map[Source]*layer
}

// LayerInput 是构造快照时一层的原始内容。
type LayerInput struct {
	Source Source
	Path   string
	Values map[string]string
}

// New 根据各层内容构造快照（传入顺序任意，解析按固定信任顺序）。
func New(layers []LayerInput) *Snapshot {
	s := &Snapshot{bySource: map[Source]*layer{}}
	for _, l := range layers {
		cp := &layer{path: l.Path, values: map[string]string{}}
		for k, v := range l.Values {
			cp.values[lookupKey(k)] = v
		}
		s.bySource[l.Source] = cp
	}
	return s
}

// Get 跨所有层按信任顺序解析一个变量。
func (s *Snapshot) Get(name string) (Entry, bool) {
	return s.GetFrom(name, sourceOrder)
}

// GetFrom 只在指定层中按固定信任顺序解析。
func (s *Snapshot) GetFrom(name string, sources []Source) (Entry, bool) {
	key := lookupKey(name)
	for _, src := range sourceOrder {
		if !contains(sources, src) {
			continue
		}
		if l, ok := s.bySource[src]; ok {
			if v, ok := l.values[key]; ok {
				return Entry{Value: v, Source: src, Path: l.path}, true
			}
		}
	}
	return Entry{}, false
}

func lookupKey(name string) string {
	if runtime.GOOS == "windows" {
		return toUpper(name)
	}
	return name
}

func toUpper(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'a' && b[i] <= 'z' {
			b[i] -= 32
		}
	}
	return string(b)
}

func contains(list []Source, v Source) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
