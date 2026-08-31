// 本文件对应任务 M47：Tool Presentation 中立 vocabulary（9 种 card）。
//
// 对齐上游：packages/core/tools/src/presentation.ts
//
// 设计要点：
//   - ToolCallView 与 ToolResultView 是「中立」的渲染意图词汇，工具通过 presentCall /
//     presentResult 声明自身卡片，UI 按其 card 切换渲染，绝不特判工具名；
//   - ToolCallView 3 种：generic / terminal / diff；ToolResultView 6 种：generic /
//     terminal / diff / search(matches|paths) / read / web(search|fetch) → 合计 9 种 card；
//   - 自定义工具可提供自定义 card 类型（card 判别为 string，不封闭）。
package tools

// ============================================================================
// 工具调用类别（ToolCallKind）
// ============================================================================

// ToolCallKind 是工具调用的类别，UI 据此选图标/样式；other 为缺省。
type ToolCallKind string

// 工具调用类别枚举。
const (
	CallKindRead    ToolCallKind = "read"
	CallKindEdit    ToolCallKind = "edit"
	CallKindDelete  ToolCallKind = "delete"
	CallKindMove    ToolCallKind = "move"
	CallKindSearch  ToolCallKind = "search"
	CallKindExecute ToolCallKind = "execute"
	CallKindFetch   ToolCallKind = "fetch"
	CallKindOther   ToolCallKind = "other"
)

// ============================================================================
// 文件位置与差异（call 与 diff 共用）
// ============================================================================

// FileLocation 是工具读取/修改的文件位置（供编辑器"跟随"）。
type FileLocation struct {
	// Path 工具操作的路径（模型向路径）。
	Path string `json:"path"`
	// Line 可选的 1-based 关注行号。
	Line int `json:"line,omitempty"`
}

// FileDiff 是工具即将做出的单文件变更（用于内联 diff 渲染）。
type FileDiff struct {
	// Path 文件路径。
	Path string `json:"path"`
	// OldText 变更前内容；新文件/覆盖时（调用时无先前内容）为 nil 表示。
	OldText *string `json:"oldText,omitempty"`
	// NewText 变更后内容。
	NewText string `json:"newText"`
}

// ============================================================================
// ToolCallView（3 种）
// ============================================================================

// ToolCallView 是待执行的调用展示（card 判别联合）。
type ToolCallView interface {
	isCallView()
}

// GenericCallView 是默认卡片：带标题的工具调用行 + 可选类别/突出输入/附加内容/位置。
type GenericCallView struct {
	Card      string          `json:"card"`
	Title     string          `json:"title"`
	Kind      ToolCallKind    `json:"kind,omitempty"`
	RawInput  any             `json:"rawInput,omitempty"`
	Content   []any           `json:"content,omitempty"`
	Locations []FileLocation  `json:"locations,omitempty"`
}

func (GenericCallView) isCallView() {}

// NewGenericCallView 构造 generic 调用卡片。
func NewGenericCallView(title string) *GenericCallView {
	return &GenericCallView{Card: "generic", Title: title, Kind: CallKindOther}
}

// TerminalCallView 是终终端命令卡片（cwd 作为头部）。
type TerminalCallView struct {
	Card        string `json:"card"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Cwd         string `json:"cwd,omitempty"`
}

func (TerminalCallView) isCallView() {}

// NewTerminalCallView 构造 terminal 调用卡片。
func NewTerminalCallView(command string) *TerminalCallView {
	return &TerminalCallView{Card: "terminal", Title: command}
}

// DiffCallView 是内联 diff 卡片（创建/修改文件）。
type DiffCallView struct {
	Card      string          `json:"card"`
	Title     string          `json:"title"`
	Diffs     []FileDiff      `json:"diffs"`
	Locations []FileLocation  `json:"locations,omitempty"`
}

func (DiffCallView) isCallView() {}

// NewDiffCallView 构造 diff 调用卡片。
func NewDiffCallView(title string, diffs []FileDiff) *DiffCallView {
	return &DiffCallView{Card: "diff", Title: title, Diffs: diffs}
}

// ============================================================================
// ToolResultView（6 种：generic/terminal/diff/search/read/web）
// ============================================================================

// ToolResultView 是完成调用展示（card 判别联合）。9 种 card 覆盖 generic/terminal/
// diff/search(matches)/search(paths)/read/web(search)/web(fetch)，另加 generic。
type ToolResultView interface {
	isResultView()
}

// GenericResultView 是默认完成卡片。
type GenericResultView struct {
	Card    string `json:"card"`
	Title   string `json:"title,omitempty"`
	Content []any  `json:"content,omitempty"`
}

func (GenericResultView) isResultView() {}

// NewGenericResultView 构造 generic 结果卡片。
func NewGenericResultView() *GenericResultView {
	return &GenericResultView{Card: "generic"}
}

// TerminalResultView 是终端完成状态。
type TerminalResultView struct {
	Card     string `json:"card"`
	Title    string `json:"title,omitempty"`
	Output   string `json:"output,omitempty"`
	ExitCode *int   `json:"exitCode,omitempty"`
	Signal   string `json:"signal,omitempty"`
}

func (TerminalResultView) isResultView() {}

// NewTerminalResultView 构造 terminal 结果卡片。
func NewTerminalResultView(output string) *TerminalResultView {
	return &TerminalResultView{Card: "terminal", Output: output}
}

// DiffResultView 是完成文件变更的 diff 卡片。
type DiffResultView struct {
	Card  string     `json:"card"`
	Title string     `json:"title,omitempty"`
	Diffs []FileDiff `json:"diffs"`
}

func (DiffResultView) isResultView() {}

// SearchLineMatch 是文件内单条匹配行。
type SearchLineMatch struct {
	LineNumber int    `json:"lineNumber"`
	Line       string `json:"line"`
}

// SearchFileMatches 是某文件的分组匹配。
type SearchFileMatches struct {
	Path    string            `json:"path"`
	Matches []SearchLineMatch `json:"matches"`
}

// SearchResultView 是完成搜索卡片（matches/paths 两种 shape）。
type SearchResultView struct {
	Card      string             `json:"card"`
	Shape     string             `json:"shape"` // "matches" | "paths"
	Title     string             `json:"title,omitempty"`
	Files     []SearchFileMatches `json:"files,omitempty"`
	Paths     []string           `json:"paths,omitempty"`
	Truncated bool               `json:"truncated"`
	Total     int                `json:"total"`
}

func (SearchResultView) isResultView() {}

// NewSearchMatchesView 构造按文件分组的搜索匹配卡片。
func NewSearchMatchesView(files []SearchFileMatches, truncated bool, total int) *SearchResultView {
	return &SearchResultView{Card: "search", Shape: "matches", Files: files, Truncated: truncated, Total: total}
}

// NewSearchPathsView 构造扁平路径列表的搜索卡片。
func NewSearchPathsView(paths []string, truncated bool, total int) *SearchResultView {
	return &SearchResultView{Card: "search", Shape: "paths", Paths: paths, Truncated: truncated, Total: total}
}

// ReadFileLine 是带 1-based 行号的单行文件内容。
type ReadFileLine struct {
	Number int    `json:"number"`
	Text   string `json:"text"`
}

// ReadResultView 是带行号代码视图的完成读卡片。
type ReadResultView struct {
	Card       string         `json:"card"`
	Title      string         `json:"title,omitempty"`
	Path       string         `json:"path"`
	Offset     int            `json:"offset"`
	Lines      []ReadFileLine `json:"lines"`
	TotalLines int            `json:"totalLines"`
	Lang       string         `json:"lang,omitempty"`
	Content    []any          `json:"content,omitempty"`
}

func (ReadResultView) isResultView() {}

// NewReadResultView 构造读文件结果卡片。
func NewReadResultView(path string, offset int, lines []ReadFileLine, totalLines int) *ReadResultView {
	return &ReadResultView{Card: "read", Path: path, Offset: offset, Lines: lines, TotalLines: totalLines}
}

// WebSource 是 web 检索结果中的单个可引用来源。
type WebSource struct {
	URL         string `json:"url"`
	Title       string `json:"title,omitempty"`
	Snippet     string `json:"snippet,omitempty"`
	PublishedAt string `json:"publishedAt,omitempty"`
}

// WebResultView 是完成 web 检索卡片（search/fetch 两种 kind）。
type WebResultView struct {
	Card      string      `json:"card"`
	Kind      string      `json:"kind"` // "search" | "fetch"
	Title     string      `json:"title,omitempty"`
	Sources   []WebSource `json:"sources,omitempty"`
	Answer    string      `json:"answer,omitempty"`
	URL       string      `json:"url,omitempty"`
	StatusCode int        `json:"statusCode,omitempty"`
	Truncated bool        `json:"truncated"`
}

func (WebResultView) isResultView() {}

// NewWebSearchResultView 构造 web_search 结果卡片。
func NewWebSearchResultView(sources []WebSource, answer string, truncated bool) *WebResultView {
	return &WebResultView{Card: "web", Kind: "search", Sources: sources, Answer: answer, Truncated: truncated}
}

// NewWebFetchResultView 构造 web_fetch 结果卡片。
func NewWebFetchResultView(url string, statusCode int, truncated bool) *WebResultView {
	return &WebResultView{Card: "web", Kind: "fetch", URL: url, StatusCode: statusCode, Truncated: truncated}
}

// ============================================================================
// CardOf 判别助手 + 自定义卡片
// ============================================================================

// CardOf 返回任意调用卡片/结果卡片的 card 判别值（自定义类型用返回值赋值）。
func CardOf(v any) string {
	switch tv := v.(type) {
	case ToolCallView:
		// 通过已声明的 card 字段统一提取。
		switch tv.(type) {
		case *GenericCallView:
			return "generic"
		case *TerminalCallView:
			return "terminal"
		case *DiffCallView:
			return "diff"
		case GenericCallView:
			return "generic"
		case TerminalCallView:
			return "terminal"
		case DiffCallView:
			return "diff"
		}
	case ToolResultView:
		switch r := tv.(type) {
		case *GenericResultView:
			return "generic"
		case *TerminalResultView:
			return "terminal"
		case *DiffResultView:
			return "diff"
		case SearchResultView:
			return "search"
		case *SearchResultView:
			return "search"
		case ReadResultView:
			return "read"
		case *ReadResultView:
			return "read"
		case WebResultView:
			return "web"
		case *WebResultView:
			return "web"
		case GenericResultView:
			return "generic"
		case TerminalResultView:
			return "terminal"
		case DiffResultView:
			return "diff"
		default:
			_ = r // 自定义结果类型：回落 other
		}
	}
	return "other"
}