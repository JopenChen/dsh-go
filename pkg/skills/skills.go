// Package skills 提供 Skill 系统（6 层 rank + 变更观察 + tool-skill）。
//
// 对齐上游：packages/core/skills + providers + tool-skill（M40）
//
// 本文件实现：
//   - Skill{name/description/rank/modelInvocable/userInvocable/content}；
//   - SkillRegistry：6 层目录注册表（project-dsh → project-agents → custom →
//     user-dsh → user-agents → bundled），rank 越 0 越大越权威，同名取高 rank 胜；
//   - 变更观察：polling watcher（Windows 友好）检测目录 hash 变化并回调（skills/change），
//     生产可替换为 fsnotify 后端；
//   - skill(name) 工具：modelInvocable/userInvocable 策略 + 注入 injected-context。
package skills

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ============================================================================
// 类型
// ============================================================================

// Skill 是一个技能。
type Skill struct {
	// Name 技能名（去扩展名的文件名）。
	Name string
	// Description 简述（markdown 首行/描述字段）。
	Description string
	// Path 源文件路径。
	Path string
	// Rank 权威层级（0 最权威，6 层：project-dsh=0 ... bundled=5）。
	Rank int
	// ModelInvocable 是否允许模型调用。
	ModelInvocable bool
	// UserInvocable 是否允许用户调用。
	UserInvocable bool
	// Content 技能正文（注入上下文用）。
	Content string
}

// SkillCandidate 是一次发现的候选（rank + locator）。
type SkillCandidate struct {
	// Rank 权威层级。
	Rank int
	// Locator 源文件定位（路径）。
	Locator string
}

// SkillLayer 是一个技能目录层。
type SkillLayer struct {
	// Title 层名（project-dsh / bundled ...）。
	Title string
	// Dir 目录路径。
	Dir string
	// Rank 权威层级。
	Rank int
}

// defaultLayers 返回基于项目根的默认 6 层目录（按 rank 0→5）。
func defaultLayers(root string) []SkillLayer {
	home, _ := os.UserHomeDir()
	return []SkillLayer{
		{Title: "project-dsh", Dir: filepath.Join(root, ".dsh", "skills"), Rank: 0},
		{Title: "project-agents", Dir: filepath.Join(root, ".agents", "skills"), Rank: 1},
		{Title: "custom", Dir: filepath.Join(root, ".config", "skills"), Rank: 2},
		{Title: "user-dsh", Dir: filepath.Join(home, ".dsh", "skills"), Rank: 3},
		{Title: "user-agents", Dir: filepath.Join(home, ".agents", "skills"), Rank: 4},
		{Title: "bundled", Dir: filepath.Join("internal", "skills"), Rank: 5},
	}
}

// SkillRegistry 是技能注册表。
type SkillRegistry struct {
	mu     sync.RWMutex
	layers []SkillLayer
	skills map[string]*Skill // name → 最高 rank 技能
}

// New 创建技能注册表（默认 6 层）。
func New(root string) *SkillRegistry {
	return &SkillRegistry{
		layers: defaultLayers(root),
		skills: map[string]*Skill{},
	}
}

// AddLayer 追加一个自定义层。
func (r *SkillRegistry) AddLayer(l SkillLayer) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.layers = append(r.layers, l)
}

// Layers 返回层列表。
func (r *SkillRegistry) Layers() []SkillLayer {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]SkillLayer{}, r.layers...)
}

// Scan 重新扫描所有层目录，合并为 name→skill（rank 高者胜）。
func (r *SkillRegistry) Scan() {
	r.mu.Lock()
	defer r.mu.Unlock()
	merged := map[string]*Skill{}
	// 按 rank 从高到低扫描；后写覆盖前写，最终保留 rank 最高者（rank 越低越权威先扫，
	// 实际后覆盖的是 rank 高者 → 我们确保 rank 高（0 权威）在最终 map 中）。
	// 简化：逐层扫描，同 name 已有 skill 且其 rank 更小（更权威）则不覆盖。
	layers := append([]SkillLayer{}, r.layers...)
	// 稳定排序：权威优先（rank 小者先，后出现者不覆盖它们）。
	sort.SliceStable(layers, func(i, j int) bool { return layers[i].Rank < layers[j].Rank })
	for _, layer := range layers {
		for _, skill := range scanDir(layer.Dir, layer.Rank) {
			if existing, ok := merged[skill.Name]; ok && existing.Rank < skill.Rank {
				// 已有更权威技能，不覆盖。
				continue
			}
			merged[skill.Name] = skill
		}
	}
	r.skills = merged
}

// List 返回全部技能（按 name 字典序）。
func (r *SkillRegistry) List() []*Skill {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.skills))
	for n := range r.skills {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]*Skill, 0, len(names))
	for _, n := range names {
		out = append(out, r.skills[n])
	}
	return out
}

// Get 按名取技能；不存在返回 (nil, false)。
func (r *SkillRegistry) Get(name string) (*Skill, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.skills[name]
	return s, ok
}

// Candidates 返回全部候选（rank/locator）。
func (r *SkillRegistry) Candidates() []SkillCandidate {
	list := r.List()
	out := make([]SkillCandidate, 0, len(list))
	for _, s := range list {
		out = append(out, SkillCandidate{Rank: s.Rank, Locator: s.Path})
	}
	return out
}

// scanDir 扫描一个层目录下的 *.md 技能文件。
func scanDir(dir string, rank int) []*Skill {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []*Skill
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".md")
		s := parseSkill(name, path, string(data), rank)
		if s != nil {
			out = append(out, s)
		}
	}
	return out
}

// parseSkill 解析技能 markdown：描述取首个 Description 行，正文为其余内容。
func parseSkill(name, path, text string, rank int) *Skill {
	lines := strings.Split(text, "\n")
	desc := ""
	modelInv := true
	userInv := true
	var bodyLines []string
	for _, ln := range lines {
		trim := strings.TrimSpace(ln)
		low := strings.ToLower(trim)
		if strings.HasPrefix(low, "description") || strings.HasPrefix(low, "**description**") {
			desc = strings.Trim(strings.TrimSpace(strings.SplitN(trim, ":", 2)[0]), "*")
			continue
		}
		if strings.Contains(low, "model-invocable: false") {
			modelInv = false
			continue
		}
		if strings.Contains(low, "user-invocable: false") {
			userInv = false
			continue
		}
		bodyLines = append(bodyLines, ln)
	}
	if desc == "" && len(bodyLines) > 0 {
		// 首个非空行作为描述。
		for _, ln := range bodyLines {
			if strings.TrimSpace(ln) != "" {
				desc = strings.TrimSpace(ln)
				break
			}
		}
	}
	return &Skill{
		Name:           name,
		Description:    desc,
		Path:           path,
		Rank:           rank,
		ModelInvocable: modelInv,
		UserInvocable:  userInv,
		Content:        strings.Join(bodyLines, "\n"),
	}
}

// Hash 计算目录变更指纹（纳入层目录 + 内容），用于 change-only 检测。
func (r *SkillRegistry) Hash() string {
	list := r.List()
	if len(list) == 0 {
		return "empty"
	}
	var sb strings.Builder
	for _, s := range list {
		sb.WriteString(fmt.Sprintf("%s@%d:%d:", s.Name, s.Rank, len(s.Content)))
	}
	// 简化：返回有序 name+len 串联（内容变化会影响 len）。
	return sb.String()
}

// ============================================================================
// N04（D3 纪律）：Skills catalog 稳定序列化 + change-only 注入
// ============================================================================

// CatalogText 生成 <available_skills> 目录稳定文本：按 name 字典序、字段顺序固定，
// 无随机 / 无时间戳，跨调用逐字节相同。
func (r *SkillRegistry) CatalogText() string {
	list := r.List() // 已按 name 字典序
	if len(list) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("<available_skills>\n")
	for _, s := range list {
		sb.WriteString("- ")
		sb.WriteString(s.Name)
		sb.WriteString(" (rank ")
		sb.WriteString(fmt.Sprint(s.Rank))
		sb.WriteString("): ")
		sb.WriteString(strings.TrimSpace(s.Description))
		sb.WriteString("\n")
	}
	sb.WriteString("</available_skills>")
	return sb.String()
}

// CatalogHash 返回 CatalogText 的稳定 sha256 十六进制。
func (r *SkillRegistry) CatalogHash() string {
	return catalogHashOf(r.CatalogText())
}

func catalogHashOf(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// Injector 是 change-only 注入器：仅在 catalog hash 变化时返回注入内容。
// 50 轮对话中 skills 不变 → 只注入 1 次（首次）；变化才重新注入。
type Injector struct {
	registry   *SkillRegistry
	lastHash   string
	injects    int
}

// NewInjector 创建变更注入器。
func NewInjector(r *SkillRegistry) *Injector {
	return &Injector{registry: r, lastHash: ""}
}

// MaybeInject 对比上次 hash：若 catalog 变化（或首次），返回注入文本并计数；否则不注入。
func (in *Injector) MaybeInject() (content string, injected bool) {
	hash := in.registry.CatalogHash()
	if hash == in.lastHash {
		return "", false
	}
	in.lastHash = hash
	in.injects++
	return in.registry.CatalogText(), true
}

// InjectCount 返回累计注入次数。
func (in *Injector) InjectCount() int { return in.injects }

// ============================================================================
// 变更观察（skills/change）
// ============================================================================

// Watch 轮询重新扫描目录：检测 hash 变化并回调 onChange（可回调写 skills/change 事件）。
// stop 关闭时停止；Windows 友好（轮询，可替换为 fsnotify 后端）。
func (r *SkillRegistry) Watch(stop <-chan struct{}, interval time.Duration, onChange func(names []string)) {
	if interval <= 0 {
		interval = time.Second
	}
	r.Scan()
	cur := r.Hash()
	for {
		select {
		case <-stop:
			return
		case <-time.After(interval):
			r.Scan()
			newHash := r.Hash()
			if newHash != cur && onChange != nil {
				onChange(currentNames(r))
			}
			cur = newHash
		}
	}
}

// currentNames 返回当前全部技能名（字典序）。
func currentNames(r *SkillRegistry) []string {
	list := r.List()
	names := make([]string, 0, len(list))
	for _, s := range list {
		names = append(names, s.Name)
	}
	return names
}

// ============================================================================
// tool-skill
// ============================================================================

// SkillTool 是 skill(name) 工具。
type SkillTool struct {
	registry *SkillRegistry
}

// NewSkillTool 创建技能工具。
func NewSkillTool(r *SkillRegistry) *SkillTool { return &SkillTool{registry: r} }

// Resolve 按名解析技能并应用 invocable 策略。
//   - 不存在 → error("skill not found")；
//   - !modelInvocable 且有 model 标志 → 拒绝模型调用；
//   - 返回技能正文（注入 injected-context user/message）。
func (t *SkillTool) Resolve(name string, byModel bool) (string, error) {
	s, ok := t.registry.Get(name)
	if !ok {
		return "", fmt.Errorf("skills: skill %q not found", name)
	}
	if byModel && !s.ModelInvocable {
		return "", fmt.Errorf("skills: %q is not model-invocable", name)
	}
	if !byModel && !s.UserInvocable {
		return "", fmt.Errorf("skills: %q is not user-invocable", name)
	}
	return s.Content, nil
}