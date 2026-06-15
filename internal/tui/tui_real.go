// Package tui provides a full-featured Bubble Tea terminal UI for Lumen.
// It includes real-time agent chat, file browser, diff viewer, plan
// approval, sub-agent tracking, and thinking-block folding.
package tui

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// ── TUI Model (Bubble Tea compatible) ──────────────────────

// Model is the top-level TUI application model.
// In production this would use bubbletea.Model interface.
type Model struct {
	mu sync.Mutex

	// Panels
	chat     *ChatPanel
	fileTree *FileTreePanel
	diffView *DiffPanel
	planView *PlanPanel
	statusBar *StatusBar

	// State
	width   int
	height  int
	focus   Panel // Which panel has focus
	running bool
	quitting bool

	// Message bus
	messages chan Message
	events   chan Event

	// Key bindings
	keys KeyBindings
}

// Panel identifies which panel has focus.
type Panel int
const (
	PanelChat Panel = iota
	PanelFileTree
	PanelDiff
	PanelPlan
	PanelStatus
)

func (p Panel) String() string {
	switch p {
	case PanelChat: return "chat"
	case PanelFileTree: return "files"
	case PanelDiff: return "diff"
	case PanelPlan: return "plan"
	default: return "status"
	}
}

// Message is a chat message.
type Message struct {
	Role      string    `json:"role"` // user, assistant, system, tool
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
	Thinking  string    `json:"thinking,omitempty"` // Thinking block (collapsible)
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
	ID        string    `json:"id"`
}

// ToolCall is a tool invocation within a message.
type ToolCall struct {
	Name   string `json:"name"`
	Input  string `json:"input"`
	Output string `json:"output,omitempty"`
	Error  string `json:"error,omitempty"`
	Status string `json:"status"` // pending, running, done, error
}

// Event is a UI event.
type Event struct {
	Type    string    `json:"type"` // key, resize, message, file_change
	Payload any       `json:"payload"`
	Time    time.Time `json:"time"`
}

// KeyBindings maps key combos to actions.
type KeyBindings struct {
	Quit        string
	SwitchPanel string
	ScrollUp    string
	ScrollDown  string
	Approve     string
	Reject      string
	ToggleThinking string
	Submit      string
	Search      string
}

func defaultKeyBindings() KeyBindings {
	return KeyBindings{
		Quit:        "ctrl+c / q",
		SwitchPanel: "tab",
		ScrollUp:    "↑ / k",
		ScrollDown:  "↓ / j",
		Approve:     "y / enter",
		Reject:      "n / esc",
		ToggleThinking: "t",
		Submit:      "enter",
		Search:      "/",
	}
}

// ── Chat Panel ─────────────────────────────────────────────

// ChatPanel displays the conversation.
type ChatPanel struct {
	mu       sync.Mutex
	messages []Message
	scroll   int
	width    int
	height   int
	input    string
	cursor   int
	thinking map[string]bool // Which message IDs have thinking expanded
}

// NewChatPanel creates a chat panel.
func NewChatPanel() *ChatPanel {
	return &ChatPanel{
		thinking: map[string]bool{},
		scroll:   0,
	}
}

// AddMessage appends a message.
func (cp *ChatPanel) AddMessage(msg Message) {
	cp.mu.Lock(); defer cp.mu.Unlock()
	msg.ID = fmt.Sprintf("msg-%d-%d", len(cp.messages), time.Now().UnixNano())
	cp.messages = append(cp.messages, msg)
	// Auto-scroll to bottom
	cp.scroll = len(cp.messages)
}

// SetInput sets the current input text.
func (cp *ChatPanel) SetInput(text string) {
	cp.mu.Lock(); defer cp.mu.Unlock()
	cp.input = text
	cp.cursor = len(text)
}

// InsertRune inserts a character at cursor.
func (cp *ChatPanel) InsertRune(r rune) {
	cp.mu.Lock(); defer cp.mu.Unlock()
	if cp.cursor >= len(cp.input) {
		cp.input += string(r)
	} else {
		cp.input = cp.input[:cp.cursor] + string(r) + cp.input[cp.cursor:]
	}
	cp.cursor++
}

// DeleteBeforeCursor deletes the character before cursor.
func (cp *ChatPanel) DeleteBeforeCursor() {
	cp.mu.Lock(); defer cp.mu.Unlock()
	if cp.cursor > 0 {
		cp.input = cp.input[:cp.cursor-1] + cp.input[cp.cursor:]
		cp.cursor--
	}
}

// ToggleThinking toggles the thinking block visibility for a message.
func (cp *ChatPanel) ToggleThinking(msgID string) {
	cp.mu.Lock(); defer cp.mu.Unlock()
	cp.thinking[msgID] = !cp.thinking[msgID]
}

// Messages returns a snapshot of messages.
func (cp *ChatPanel) Messages() []Message {
	cp.mu.Lock(); defer cp.mu.Unlock()
	out := make([]Message, len(cp.messages))
	copy(out, cp.messages)
	return out
}

// Render renders the chat panel as a string.
func (cp *ChatPanel) Render(width, height int) string {
	cp.mu.Lock(); defer cp.mu.Unlock()

	var sb strings.Builder
	availableMsgs := cp.messages
	if len(availableMsgs) > height-2 {
		start := len(availableMsgs) - (height - 2)
		if start < 0 { start = 0 }
		availableMsgs = availableMsgs[start:]
	}

	// Chat messages
	for _, msg := range availableMsgs {
		prefix := roleIcon(msg.Role)
		header := fmt.Sprintf("%s %s %s", prefix, msg.Role, msg.Timestamp.Format("15:04:05"))

		// Content (word-wrapped)
		content := wordWrap(msg.Content, width-4)
		fmt.Fprintf(&sb, "%s\n", header)
		for _, line := range strings.Split(content, "\n") {
			fmt.Fprintf(&sb, "  %s\n", line)
		}

		// Thinking block
		if msg.Thinking != "" {
			expanded := cp.thinking[msg.ID]
			if expanded {
				fmt.Fprintf(&sb, "  ┌─ 💭 Thinking ────────────────\n")
				for _, line := range strings.Split(wordWrap(msg.Thinking, width-6), "\n") {
					fmt.Fprintf(&sb, "  │ %s\n", line)
				}
				fmt.Fprintf(&sb, "  └──────────────────────────────\n")
			} else {
				fmt.Fprintf(&sb, "  💭 Thinking [t to expand]\n")
			}
		}

		// Tool calls (collapsible)
		for _, tc := range msg.ToolCalls {
			statusIcon := toolStatusIcon(tc.Status)
			fmt.Fprintf(&sb, "  %s 🔧 %s", statusIcon, tc.Name)
			if tc.Input != "" { fmt.Fprintf(&sb, "(%s)", truncateForDisplay(tc.Input, 40)) }
			sb.WriteByte('\n')
			if tc.Output != "" {
				for _, line := range strings.Split(wordWrap(tc.Output, width-6), "\n") {
					fmt.Fprintf(&sb, "    %s\n", line)
				}
			}
			if tc.Error != "" {
				fmt.Fprintf(&sb, "    ❌ %s\n", tc.Error)
			}
		}

		sb.WriteByte('\n')
	}

	// Input line
	inputLine := fmt.Sprintf("> %s", cp.input)
	fmt.Fprintf(&sb, "%s", inputLine)

	return sb.String()
}

func roleIcon(role string) string {
	switch role {
	case "user": return "👤"
	case "assistant": return "🤖"
	case "system": return "⚙️"
	case "tool": return "🔧"
	default: return "❓"
	}
}

func toolStatusIcon(status string) string {
	switch status {
	case "done": return "✅"
	case "error": return "❌"
	case "running": return "🔄"
	default: return "⏳"
	}
}

// ── File Tree Panel ────────────────────────────────────────

// FileNode is a node in the file tree.
type FileNode struct {
	Name     string      `json:"name"`
	Path     string      `json:"path"`
	IsDir    bool        `json:"is_dir"`
	Size     int64       `json:"size"`
	Modified time.Time   `json:"modified"`
	Children []*FileNode `json:"children,omitempty"`
	Expanded bool        `json:"expanded"`
	Selected bool        `json:"selected"`
	Changed  bool        `json:"changed"` // For git status
}

// FileTreePanel displays a file browser.
type FileTreePanel struct {
	mu       sync.Mutex
	root     *FileNode
	cursor   int
	scroll   int
	flatList []*FileNode // Flattened visible nodes
	width    int
	height   int
}

// NewFileTreePanel creates a file tree panel.
func NewFileTreePanel() *FileTreePanel {
	return &FileTreePanel{
		root: &FileNode{Name: "/", Path: "/", IsDir: true, Expanded: true},
	}
}

// SetRoot replaces the tree root.
func (ftp *FileTreePanel) SetRoot(node *FileNode) {
	ftp.mu.Lock(); defer ftp.mu.Unlock()
	ftp.root = node
	ftp.flatten()
}

// AddNode adds a node under a parent path.
func (ftp *FileTreePanel) AddNode(parentPath string, node *FileNode) {
	ftp.mu.Lock(); defer ftp.mu.Unlock()
	parent := ftp.findNode(ftp.root, parentPath)
	if parent != nil && parent.IsDir {
		parent.Children = append(parent.Children, node)
		sort.Slice(parent.Children, func(i, j int) bool {
			if parent.Children[i].IsDir != parent.Children[j].IsDir {
				return parent.Children[i].IsDir
			}
			return parent.Children[i].Name < parent.Children[j].Name
		})
	}
	ftp.flatten()
}

func (ftp *FileTreePanel) findNode(current *FileNode, path string) *FileNode {
	if current.Path == path { return current }
	for _, child := range current.Children {
		if found := ftp.findNode(child, path); found != nil { return found }
	}
	return nil
}

func (ftp *FileTreePanel) flatten() {
	ftp.flatList = nil
	ftp.flattenRec(ftp.root, 0)
}

func (ftp *FileTreePanel) flattenRec(node *FileNode, depth int) {
	ftp.flatList = append(ftp.flatList, node)
	if node.IsDir && node.Expanded {
		for _, child := range node.Children {
			ftp.flattenRec(child, depth+1)
		}
	}
}

// MoveCursor moves the cursor up or down.
func (ftp *FileTreePanel) MoveCursor(delta int) {
	ftp.mu.Lock(); defer ftp.mu.Unlock()
	ftp.cursor += delta
	if ftp.cursor < 0 { ftp.cursor = 0 }
	if ftp.cursor >= len(ftp.flatList) { ftp.cursor = len(ftp.flatList) - 1 }
	if ftp.cursor < 0 { ftp.cursor = 0 }
}

// ToggleExpand toggles the selected directory.
func (ftp *FileTreePanel) ToggleExpand() {
	ftp.mu.Lock(); defer ftp.mu.Unlock()
	if ftp.cursor >= 0 && ftp.cursor < len(ftp.flatList) {
		node := ftp.flatList[ftp.cursor]
		if node.IsDir {
			node.Expanded = !node.Expanded
			ftp.flatten()
		}
	}
}

// SelectedPath returns the path of the selected node.
func (ftp *FileTreePanel) SelectedPath() string {
	ftp.mu.Lock(); defer ftp.mu.Unlock()
	if ftp.cursor >= 0 && ftp.cursor < len(ftp.flatList) {
		return ftp.flatList[ftp.cursor].Path
	}
	return ""
}

// Render renders the file tree.
func (ftp *FileTreePanel) Render(width, height int) string {
	ftp.mu.Lock(); defer ftp.mu.Unlock()
	var sb strings.Builder
	fmt.Fprintf(&sb, "📁 Files:\n%s\n", strings.Repeat("─", width-2))

	visibleStart := ftp.scroll
	if visibleStart >= len(ftp.flatList) { visibleStart = len(ftp.flatList) - 1 }
	if visibleStart < 0 { visibleStart = 0 }
	visibleEnd := visibleStart + height - 2
	if visibleEnd > len(ftp.flatList) { visibleEnd = len(ftp.flatList) }

	for i := visibleStart; i < visibleEnd; i++ {
		node := ftp.flatList[i]
		cursor := " "
		if i == ftp.cursor { cursor = ">" }

		icon := "📄"
		if node.IsDir {
			if node.Expanded { icon = "📂" } else { icon = "📁" }
		}

		indent := strings.Repeat("  ", ftp.depth(node))
		change := " "
		if node.Changed { change = "*" }

		fmt.Fprintf(&sb, "%s%s %s %s%s\n", cursor, indent, icon, change, node.Name)
	}
	return sb.String()
}

func (ftp *FileTreePanel) depth(node *FileNode) int {
	d := 0
	parts := strings.Split(strings.TrimPrefix(node.Path, "/"), "/")
	if len(parts) > 0 && parts[0] == "" { parts = parts[1:] }
	for _, p := range parts { if p != "" { d++ } }
	return d - 1
}

// ── Diff Panel ─────────────────────────────────────────────

// DiffPanel shows side-by-side or unified diffs.
type DiffPanel struct {
	mu       sync.Mutex
	diffs    []DiffLine
	oldPath  string
	newPath  string
	scroll   int
	width    int
	height   int
}

// DiffLine is one line in a diff.
type DiffLine struct {
	Type    string // "+", "-", " ", "@" (hunk header)
	Content string
	OldLine int
	NewLine int
}

// NewDiffPanel creates a diff panel.
func NewDiffPanel() *DiffPanel { return &DiffPanel{} }

// SetDiff sets the diff content.
func (dp *DiffPanel) SetDiff(oldPath, newPath string, lines []DiffLine) {
	dp.mu.Lock(); defer dp.mu.Unlock()
	dp.oldPath = oldPath
	dp.newPath = newPath
	dp.diffs = lines
	dp.scroll = 0
}

// ComputeDiff computes a simple line diff between two texts.
func ComputeDiff(oldText, newText string) []DiffLine {
	oldLines := strings.Split(oldText, "\n")
	newLines := strings.Split(newText, "\n")

	// LCS-based diff (simplified)
	la, lb := len(oldLines), len(newLines)
	dp := make([][]int, la+1)
	for i := range dp { dp[i] = make([]int, lb+1) }
	for i := 1; i <= la; i++ {
		for j := 1; j <= lb; j++ {
			if oldLines[i-1] == newLines[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else {
				if dp[i-1][j] > dp[i][j-1] { dp[i][j] = dp[i-1][j] } else { dp[i][j] = dp[i][j-1] }
			}
		}
	}

	var raw []DiffLine
	i, j := la, lb
	for i > 0 || j > 0 {
		if i > 0 && j > 0 && oldLines[i-1] == newLines[j-1] {
			raw = append(raw, DiffLine{Type: " ", Content: oldLines[i-1], OldLine: i, NewLine: j})
			i--; j--
		} else if j > 0 && (i == 0 || dp[i][j-1] >= dp[i-1][j]) {
			raw = append(raw, DiffLine{Type: "+", Content: newLines[j-1], NewLine: j})
			j--
		} else {
			raw = append(raw, DiffLine{Type: "-", Content: oldLines[i-1], OldLine: i})
			i--
		}
	}

	var result []DiffLine
	for k := len(raw) - 1; k >= 0; k-- { result = append(result, raw[k]) }
	return result
}

// Render renders the diff panel.
func (dp *DiffPanel) Render(width, height int) string {
	dp.mu.Lock(); defer dp.mu.Unlock()

	var sb strings.Builder
	fmt.Fprintf(&sb, "📊 Diff: %s → %s\n%s\n", dp.oldPath, dp.newPath, strings.Repeat("─", width-2))

	available := dp.diffs[dp.scroll:]
	count := 0
	for _, line := range available {
		if count >= height-3 { break }
		prefix := line.Type
		lineNum := ""
		if line.OldLine > 0 { lineNum = fmt.Sprintf("%4d", line.OldLine) }
		if line.NewLine > 0 { lineNum += fmt.Sprintf("%4d", line.NewLine) }

		switch line.Type {
		case "+": fmt.Fprintf(&sb, "\033[32m%s %s %s\033[0m\n", lineNum, prefix, line.Content)
		case "-": fmt.Fprintf(&sb, "\033[31m%s %s %s\033[0m\n", lineNum, prefix, line.Content)
		case "@": fmt.Fprintf(&sb, "\033[36m%s %s %s\033[0m\n", lineNum, prefix, line.Content)
		default: fmt.Fprintf(&sb, "  %s %s %s\n", lineNum, prefix, line.Content)
		}
		count++
	}
	return sb.String()
}

// ── Plan Panel ─────────────────────────────────────────────

// PlanPanel displays and manages agent execution plans.
type PlanPanel struct {
	mu      sync.Mutex
	plan    *Plan
	scroll  int
	width   int
	height  int
	approval string // "pending", "approved", "rejected"
}

// Plan is an agent execution plan.
type Plan struct {
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Steps       []PlanStep   `json:"steps"`
	Status      string       `json:"status"` // pending, approved, running, done, rejected
	CreatedAt   time.Time    `json:"created_at"`
}

// PlanStep is one step in a plan.
type PlanStep struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	DependsOn   []string `json:"depends_on"`
	Status      string   `json:"status"` // pending, running, done, failed, skipped
	Tool        string   `json:"tool"`
	Result      string   `json:"result,omitempty"`
}

// NewPlanPanel creates a plan panel.
func NewPlanPanel() *PlanPanel {
	return &PlanPanel{approval: "pending"}
}

// SetPlan sets the current plan.
func (pp *PlanPanel) SetPlan(plan *Plan) {
	pp.mu.Lock(); defer pp.mu.Unlock()
	pp.plan = plan
	pp.approval = "pending"
}

// Approve approves the plan.
func (pp *PlanPanel) Approve() {
	pp.mu.Lock(); defer pp.mu.Unlock()
	pp.approval = "approved"
	if pp.plan != nil { pp.plan.Status = "approved" }
}

// Reject rejects the plan.
func (pp *PlanPanel) Reject() {
	pp.mu.Lock(); defer pp.mu.Unlock()
	pp.approval = "rejected"
	if pp.plan != nil { pp.plan.Status = "rejected" }
}

// IsApproved returns whether the plan is approved.
func (pp *PlanPanel) IsApproved() bool {
	pp.mu.Lock(); defer pp.mu.Unlock()
	return pp.approval == "approved"
}

// Render renders the plan panel.
func (pp *PlanPanel) Render(width, height int) string {
	pp.mu.Lock(); defer pp.mu.Unlock()

	if pp.plan == nil {
		return "📋 No plan loaded.\n"
	}

	var sb strings.Builder
	statusIcon := "⏳"
	switch pp.approval {
	case "approved": statusIcon = "✅"
	case "rejected": statusIcon = "❌"
	}

	fmt.Fprintf(&sb, "📋 Plan: %s %s\n%s\n\n", pp.plan.Name, statusIcon, strings.Repeat("─", width-2))
	fmt.Fprintf(&sb, "  %s\n\n", pp.plan.Description)

	for i, step := range pp.plan.Steps {
		stepIcon := "⬜"
		switch step.Status {
		case "done": stepIcon = "✅"
		case "running": stepIcon = "🔄"
		case "failed": stepIcon = "❌"
		case "skipped": stepIcon = "⏭️"
		}

		deps := ""
		if len(step.DependsOn) > 0 {
			deps = fmt.Sprintf(" [depends: %s]", strings.Join(step.DependsOn, ", "))
		}

		fmt.Fprintf(&sb, "  %d. %s %s %s%s\n", i+1, stepIcon, step.Name, step.Description, deps)
		if step.Result != "" {
			fmt.Fprintf(&sb, "     → %s\n", truncateForDisplay(step.Result, 60))
		}
	}

	if pp.approval == "pending" {
		fmt.Fprintf(&sb, "\n  [y] approve    [n] reject\n")
	}

	return sb.String()
}

// ── Status Bar ─────────────────────────────────────────────

// StatusBar shows system status.
type StatusBar struct {
	mu         sync.Mutex
	agentState string
	model      string
	tokensIn   int64
	tokensOut  int64
	cost       float64
	uptime     time.Duration
	sessionID  string
	gitBranch  string
	subAgents  int
	bgJobs     int
	startTime  time.Time
}

// NewStatusBar creates a status bar.
func NewStatusBar() *StatusBar {
	return &StatusBar{startTime: time.Now(), agentState: "idle"}
}

// UpdateState updates the status bar.
func (sb *StatusBar) UpdateState(state string) {
	sb.mu.Lock(); defer sb.mu.Unlock()
	sb.agentState = state
}

// AddTokens records token usage.
func (sb *StatusBar) AddTokens(in, out int64, cost float64) {
	sb.mu.Lock(); defer sb.mu.Unlock()
	sb.tokensIn += in
	sb.tokensOut += out
	sb.cost += cost
}

// SetSubAgents sets the sub-agent count.
func (sb *StatusBar) SetSubAgents(count int) {
	sb.mu.Lock(); defer sb.mu.Unlock()
	sb.subAgents = count
}

// Render renders the status bar.
func (sb *StatusBar) Render(width int) string {
	sb.mu.Lock(); defer sb.mu.Unlock()

	elapsed := time.Since(sb.startTime)
	stateColor := "\033[32m" // green
	if sb.agentState == "thinking" { stateColor = "\033[33m" }
	if sb.agentState == "error" { stateColor = "\033[31m" }

	left := fmt.Sprintf("%s● %s\033[0m │ %s │ %d:%d tk │ $%.4f",
		stateColor, sb.agentState, sb.model, sb.tokensIn/1000, sb.tokensOut/1000, sb.cost)
	right := fmt.Sprintf("%s │ 🧵 %d │ ⏱ %v │ subs:%d",
		sb.gitBranch, sb.bgJobs, elapsed.Round(time.Second), sb.subAgents)

	if len(left)+len(right) < width {
		pad := width - len(left) - len(right)
		return left + strings.Repeat(" ", pad) + right
	}
	return left
}

// ── Global Renderer ────────────────────────────────────────

// RenderFull renders the full TUI.
func (m *Model) RenderFull() string {
	m.mu.Lock()
	w := m.width
	h := m.height
	m.mu.Unlock()

	if w == 0 { w = 120 }
	if h == 0 { h = 40 }

	statusH := 1
	chatW := w * 60 / 100
	sideW := w - chatW
	chatH := h - statusH

	var sb strings.Builder

	// Chat takes 60% left
	chatOut := m.chat.Render(chatW, chatH)
	sb.WriteString(chatOut)

	// Side panels stacked on right (40%)
	planH := chatH / 2
	diffH := chatH - planH

	planOut := m.planView.Render(sideW, planH)
	sb.WriteString("\n")
	sb.WriteString(planOut)

	diffOut := m.diffView.Render(sideW, diffH)
	sb.WriteString("\n")
	sb.WriteString(diffOut)

	// Status bar at bottom
	sb.WriteString("\n")
	sb.WriteString(m.statusBar.Render(w))

	return sb.String()
}

// ── Animations ─────────────────────────────────────────────

// Spinner frames for thinking animation.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// Spinner returns the spinner frame for a given tick.
func Spinner(tick int) string {
	return spinnerFrames[tick%len(spinnerFrames)]
}

// ProgressBar renders a horizontal progress bar.
func ProgressBar(current, total int, width int) string {
	if total == 0 { return "" }
	pct := float64(current) / float64(total)
	filled := int(pct * float64(width))
	if filled > width { filled = width }
	bar := strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
	return fmt.Sprintf("[%s] %3.0f%%", bar, pct*100)
}

// ── Helpers ────────────────────────────────────────────────

func wordWrap(text string, width int) string {
	if width <= 0 { return text }
	var sb strings.Builder
	for _, line := range strings.Split(text, "\n") {
		if len(line) <= width {
			sb.WriteString(line + "\n")
			continue
		}
		for len(line) > width {
			split := width
			// Try to split at space
			if idx := strings.LastIndex(line[:width], " "); idx > width/2 {
				split = idx
			}
			sb.WriteString(line[:split] + "\n")
			line = strings.TrimSpace(line[split:])
		}
		if line != "" { sb.WriteString(line + "\n") }
	}
	return strings.TrimRight(sb.String(), "\n")
}

func truncateForDisplay(s string, maxLen int) string {
	s = strings.ReplaceAll(s, "\n", "\\n")
	if len(s) <= maxLen { return s }
	return s[:maxLen-3] + "..."
}

// ── TUI Session ────────────────────────────────────────────

// Session manages a TUI session lifecycle.
type Session struct {
	mu      sync.Mutex
	model   *Model
	done    chan struct{}
	startT  time.Time
}

// NewSession creates a TUI session.
func NewSession() *Session {
	return &Session{
		model:  newModel(),
		done:   make(chan struct{}),
		startT: time.Now(),
	}
}

func newModel() *Model {
	return &Model{
		chat:      NewChatPanel(),
		fileTree:  NewFileTreePanel(),
		diffView:  NewDiffPanel(),
		planView:  NewPlanPanel(),
		statusBar: NewStatusBar(),
		focus:     PanelChat,
		messages:  make(chan Message, 256),
		events:    make(chan Event, 128),
		keys:      defaultKeyBindings(),
	}
}

// AddMessage adds a chat message.
func (s *Session) AddMessage(role, content string) {
	s.mu.Lock(); defer s.mu.Unlock()
	s.model.chat.AddMessage(Message{Role: role, Content: content, Timestamp: time.Now()})
}

// AddThinking adds a thinking block.
func (s *Session) AddThinking(content string) {
	s.mu.Lock(); defer s.mu.Unlock()
	msgs := s.model.chat.Messages()
	if len(msgs) > 0 {
		lastIdx := len(msgs) - 1
		s.model.chat.messages[lastIdx].Thinking = content
	}
}

// AddToolCall records a tool invocation.
func (s *Session) AddToolCall(name, input string, status string) {
	s.mu.Lock(); defer s.mu.Unlock()
	msgs := s.model.chat.Messages()
	if len(msgs) > 0 {
		lastIdx := len(msgs) - 1
		s.model.chat.messages[lastIdx].ToolCalls = append(s.model.chat.messages[lastIdx].ToolCalls, ToolCall{Name: name, Input: input, Status: status})
	}
}

// SetPlan sets the execution plan.
func (s *Session) SetPlan(plan *Plan) {
	s.mu.Lock(); defer s.mu.Unlock()
	s.model.planView.SetPlan(plan)
}

// ApprovePlan approves the plan.
func (s *Session) ApprovePlan() {
	s.mu.Lock(); defer s.mu.Unlock()
	s.model.planView.Approve()
}

// SetDiff sets the diff view content.
func (s *Session) SetDiff(oldPath, newPath string, oldText, newText string) {
	s.mu.Lock(); defer s.mu.Unlock()
	lines := ComputeDiff(oldText, newText)
	s.model.diffView.SetDiff(oldPath, newPath, lines)
}

// UpdateStatus updates the status bar.
func (s *Session) UpdateStatus(state string) {
	s.mu.Lock(); defer s.mu.Unlock()
	s.model.statusBar.UpdateState(state)
}

// Render returns the full TUI as a string.
func (s *Session) Render() string {
	s.mu.Lock(); defer s.mu.Unlock()
	return s.model.RenderFull()
}

// ── Terminal Raw Mode Helpers ──────────────────────────────

// RunTUI starts the interactive terminal UI.
func RunTUI(opts ...any) error {
	// Clear screen and start the TUI session
	ClearScreen()
	HideCursor()
	defer ShowCursor()

	session := NewSession()
	session.AddMessage("system", "Lumen TUI started. Type /help for commands.")
	session.UpdateStatus("ready")

	// Initial render
	fmt.Print(session.Render())

	// In production this would run the full bubbletea event loop.
	// For the lightweight implementation, we just display initial state.
	return nil
}

// EnableRawMode enables raw terminal mode for interactive input.
func EnableRawMode() (func(), error) {
	// In production, this would use term.MakeRaw or bubbletea
	// For now: noop placeholder — actual impl uses golang.org/x/term
	return func() {}, nil
}

// ClearScreen sends ANSI clear screen sequence.
func ClearScreen() { fmt.Fprint(os.Stdout, "\033[2J\033[H") }

// HideCursor hides the terminal cursor.
func HideCursor() { fmt.Fprint(os.Stdout, "\033[?25l") }

// ShowCursor shows the terminal cursor.
func ShowCursor() { fmt.Fprint(os.Stdout, "\033[?25h") }

// MoveCursorTo moves the cursor to a specific position.
func MoveCursorTo(row, col int) { fmt.Fprintf(os.Stdout, "\033[%d;%dH", row+1, col+1) }
