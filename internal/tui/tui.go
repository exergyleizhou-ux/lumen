// Package tui provides a Bubble Tea terminal UI for Lumen.
// Multi-panel layout: chat (left 60%), plan+diff (right 40%), status bar (bottom).
package tui

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ── Styles ─────────────────────────────────────────────────

var (
	subtle  = lipgloss.AdaptiveColor{Light: "#D9DCCF", Dark: "#383838"}
	highlight = lipgloss.AdaptiveColor{Light: "#874BFD", Dark: "#7D56F4"}
	special   = lipgloss.AdaptiveColor{Light: "#43BF6D", Dark: "#73F59F"}
	dim       = lipgloss.NewStyle().Foreground(subtle)
	bold      = lipgloss.NewStyle().Bold(true)
	accent    = lipgloss.NewStyle().Foreground(highlight).Bold(true)
	green     = lipgloss.NewStyle().Foreground(special)
	red       = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	cyan      = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	white     = lipgloss.NewStyle().Foreground(lipgloss.Color("15"))

	panelBorder = lipgloss.Border{
		Top: "─", Bottom: "─", Left: "│", Right: "│",
		TopLeft: "╭", TopRight: "╮", BottomLeft: "╰", BottomRight: "╯",
	}
	panelStyle = lipgloss.NewStyle().
			Border(panelBorder).BorderForeground(subtle).
			Padding(0, 1)
	panelActiveStyle = lipgloss.NewStyle().
			Border(panelBorder).BorderForeground(highlight).
			Padding(0, 1)
	statusStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("8")).
			Foreground(lipgloss.Color("15")).
			Padding(0, 2)
	statusModeBypass  = lipgloss.NewStyle().Background(lipgloss.Color("1")).Foreground(lipgloss.Color("15")).Padding(0, 1)
	statusModePlan    = lipgloss.NewStyle().Background(lipgloss.Color("3")).Foreground(lipgloss.Color("0")).Padding(0, 1)
	statusModeDefault = lipgloss.NewStyle().Background(lipgloss.Color("6")).Foreground(lipgloss.Color("0")).Padding(0, 1)
	statusModeAccept  = lipgloss.NewStyle().Background(lipgloss.Color("2")).Foreground(lipgloss.Color("0")).Padding(0, 1)

	toolIconStyle = map[string]string{
		"read_file": "📖", "grep": "🔍", "glob": "🔍", "ls": "📂",
		"lsp_hover": "🔬", "lsp_definition": "🔬", "lsp_references": "🔬", "lsp_diagnostics": "🔬",
		"write_file": "✏️", "edit_file": "✏️", "multi_edit": "✏️", "notebook_edit": "✏️",
		"bash": "⚡", "web_fetch": "🌐", "ask": "❓",
		"todo_write": "📋", "complete_step": "✅",
	}
)

func toolIcon(name string) string {
	if ic, ok := toolIconStyle[name]; ok {
		return ic
	}
	if strings.HasPrefix(name, "github") { return "🐙" }
	if strings.HasPrefix(name, "screen") || strings.HasPrefix(name, "click") || strings.HasPrefix(name, "ui_") { return "🖥" }
	if strings.HasPrefix(name, "mcp") { return "🔌" }
	return "🔧"
}

// ── Message types ──────────────────────────────────────────

// TuiMsg is a message from the controller to the TUI.
type TuiMsg struct {
	Role      string    // "user", "assistant", "system", "tool"
	Content   string
	Thinking  string
	ToolCalls []ToolCall
	Timestamp time.Time
	ID        string
}

// ToolCall is one tool invocation.
type ToolCall struct {
	Name   string
	Input  string
	Output string
	Error  string
	Status string // pending | running | done | error | blocked
	Step   int
}

// PlanMsg carries plan data to the TUI.
type PlanMsg struct {
	Title       string
	Description string
	Steps       []PlanStepMsg
	Approved    bool
}

// PlanStepMsg is one step in a plan.
type PlanStepMsg struct {
	Number      int
	Description string
	Tool        string
	Status      string // pending | running | done | error
}

// DiffMsg carries a file diff for display.
type DiffMsg struct {
	FilePath string
	OldText  string
	NewText  string
}

// StatusMsg updates the status bar.
type StatusMsg struct {
	Model      string
	Provider   string
	Mode       string
	TokensIn   int64
	TokensOut  int64
	CacheHit   int64
	Cost       float64
	Steps      int64
	Turns      int64
	PlanMode   bool
	SubAgents  int
	State      string // "thinking", "running", "idle", "error"
}

// VerifyMsg updates the verify-after-edit indicator in the status bar. It is a
// separate message from StatusMsg so partial verify updates never clobber the
// rest of the bar. State is "running" | "ok" | "fail" ("" clears it); Detail is
// a short human summary used for the "fail" state, e.g. "build failed (2)".
type VerifyMsg struct {
	State  string
	Detail string
}

// ── Entry types ────────────────────────────────────────────

// ChatEntry is one visible line/block in the chat panel.
type ChatEntry struct {
	Kind      string // "text", "thinking", "tool", "divider"
	Role      string
	Content   string
	Tool      ToolCall
	Thinking  string
	Timestamp time.Time
}

// ── TUI Model ──────────────────────────────────────────────

// Model implements tea.Model for the Lumen TUI.
type Model struct {
	width  int
	height int
	ready  bool

	// Panels
	chat    ChatPanel
	plan    PlanPanel
	diff    DiffPanel
	status  StatusBar

	// Which panel has focus
	focusPanel int
	// focusPanel constants
	// 0 = chat, 1 = plan, 2 = diff

	// Input state (chat panel)
	input         strings.Builder
	cursorPos     int

	// Scroll
	chatScroll    int

	// Quit
	quitting bool

	// Spinner for "thinking"
	spinnerTick int

	// Message channel from controller
	msgChan chan any

	// Input channel to external controller
	inputCh chan string

	// Lock
	mu sync.Mutex
}

// ChatPanel holds chat entries.
type ChatPanel struct {
	entries     []ChatEntry
	scrollPos   int
	maxVisible  int
}

// PlanPanel holds current plan state.
type PlanPanel struct {
	plan     *PlanMsg
	reviewed bool
}

// DiffPanel holds diff state.
type DiffPanel struct {
	diff    *DiffMsg
	lines   []string
}

// StatusBar holds live status.
type StatusBar struct {
	status StatusMsg
	// verify-after-edit indicator, fed by VerifyMsg (independent of status).
	verifyState  string // "" | "running" | "ok" | "fail"
	verifyDetail string
}

// ── Constructor ────────────────────────────────────────────

// NewModel creates a new TUI model.
func NewModel() *Model {
	return &Model{
		chat: ChatPanel{entries: make([]ChatEntry, 0, 256)},
		msgChan: make(chan any, 64),
	}
}

// Send pushes a message into the TUI from outside the tea loop.
func (m *Model) Send(msg any) {
	select {
	case m.msgChan <- msg:
	default:
		// Drop if channel full — TUI not consuming fast enough.
	}
}

// ── Bubble Tea Interface ───────────────────────────────────

func (m *Model) Init() tea.Cmd {
	return listenMsgs(m.msgChan)
}

func listenMsgs(ch chan any) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
	}
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKey(msg)
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ready = true
		return m, nil
	case TuiMsg:
		m.addChatEntry(msg)
		return m, listenMsgs(m.msgChan)
	case PlanMsg:
		m.plan.plan = &msg
		m.plan.reviewed = false
		return m, listenMsgs(m.msgChan)
	case DiffMsg:
		m.diff.diff = &msg
		m.diff.lines = nil // Will compute in View
		return m, listenMsgs(m.msgChan)
	case StatusMsg:
		m.status.status = msg

	case VerifyMsg:
		m.status.verifyState = msg.State
		m.status.verifyDetail = msg.Detail
		return m, listenMsgs(m.msgChan)
	case spinnerTick:
		m.spinnerTick++
		return m, tickSpinner()
	case tea.QuitMsg:
		return m, tea.Quit
	}
	return m, listenMsgs(m.msgChan)
}

type spinnerTick struct{}

func tickSpinner() tea.Cmd {
	return tea.Tick(time.Millisecond*80, func(t time.Time) tea.Msg {
		return spinnerTick{}
	})
}

func (m *Model) addChatEntry(msg TuiMsg) {
	m.mu.Lock()
	defer m.mu.Unlock()

	entry := ChatEntry{
		Role:      msg.Role,
		Content:   msg.Content,
		Timestamp: msg.Timestamp,
		Kind:      "text",
	}

	if msg.Thinking != "" {
		entry.Thinking = msg.Thinking
		entry.Kind = "thinking"
	}
	if len(msg.ToolCalls) > 0 {
		entry.Tool = msg.ToolCalls[0]
		entry.Kind = "tool"
	}
	if msg.Role == "system" {
		entry.Kind = "divider"
	}

	m.chat.entries = append(m.chat.entries, entry)
	// Auto-scroll to bottom
	m.chat.scrollPos = len(m.chat.entries)
}

func (m *Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Handle typed characters (Runes is non-empty for regular keypresses)
	if len(msg.Runes) > 0 && m.focusPanel == 0 {
		for _, r := range msg.Runes {
			m.input.WriteRune(r)
		}
		return m, nil
	}

	switch msg.String() {
	case "ctrl+c", "esc":
		m.quitting = true
		return m, tea.Quit
	case "tab":
		m.focusPanel = (m.focusPanel + 1) % 3
		return m, nil
	case "up", "k":
		if m.focusPanel == 0 {
			if m.chat.scrollPos > 0 {
				m.chat.scrollPos--
			}
		}
		return m, nil
	case "down", "j":
		if m.focusPanel == 0 {
			if m.chat.scrollPos < len(m.chat.entries)-1 {
				m.chat.scrollPos++
			}
		}
		return m, nil
	case "pgup":
		if m.focusPanel == 0 {
			m.chat.scrollPos -= 10
			if m.chat.scrollPos < 0 {
				m.chat.scrollPos = 0
			}
		}
		return m, nil
	case "pgdown":
		if m.focusPanel == 0 {
			m.chat.scrollPos += 10
			max := len(m.chat.entries) - 1
			if m.chat.scrollPos > max {
				m.chat.scrollPos = max
			}
		}
		return m, nil
	case "enter":
		if m.focusPanel == 0 {
			line := strings.TrimSpace(m.input.String())
			m.input.Reset()
			if line != "" {
				// Display user message in chat
				m.addChatEntry(TuiMsg{Role: "user", Content: line})
				// Send to external controller
				m.mu.Lock()
				if m.inputCh != nil {
					select {
					case m.inputCh <- line:
					default:
					}
				}
				m.mu.Unlock()
			}
		}
		if m.focusPanel == 1 && m.plan.plan != nil && !m.plan.plan.Approved {
			m.plan.plan.Approved = true
		}
		return m, nil
	case "backspace":
		if m.focusPanel == 0 {
			s := m.input.String()
			if len(s) > 0 {
				runes := []rune(s)
				m.input.Reset()
				m.input.WriteString(string(runes[:len(runes)-1]))
			}
		}
		if m.focusPanel == 2 && m.diff.diff != nil {
			m.diff.diff = nil
		}
		return m, nil
	}
	return m, nil
}

// ── View ───────────────────────────────────────────────────

func (m *Model) View() string {
	if m.quitting {
		return ""
	}
	if !m.ready {
		return dim.Render("  loading…")
	}

	w := m.width
	h := m.height
	if w < 40 { w = 40 }
	if h < 10 { h = 10 }

	statusH := 1
	bodyH := h - statusH

	chatW := w * 60 / 100
	sideW := w - chatW
	planH := bodyH / 2
	diffH := bodyH - planH

	// Status bar
	statusView := m.renderStatus(w)

	// Chat panel
	chatView := m.renderChat(chatW, bodyH)

	// Plan panel
	planView := m.renderPlan(sideW, planH)

	// Diff panel
	diffView := m.renderDiff(sideW, diffH)

	// Compose: left=chat (60%), right=plan+diff (40%)
	rightCol := lipgloss.JoinVertical(lipgloss.Top, planView, diffView)
	body := lipgloss.JoinHorizontal(lipgloss.Top, chatView, rightCol)

	return lipgloss.JoinVertical(lipgloss.Top, body, statusView)
}

// ── Panel Renderers ────────────────────────────────────────

func (m *Model) renderChat(w, h int) string {
	style := panelStyle
	if m.focusPanel == 0 {
		style = panelActiveStyle
	}

	innerW := w - 4 // Account for border + padding
	innerH := h - 2

	// Available lines for messages (reserve 1 for input line)
	msgH := innerH - 1
	if msgH < 1 { msgH = 1 }

	entries := m.chat.entries
	scrollPos := m.chat.scrollPos

	// Build visible lines
	var lines []string
	for i := scrollPos; i < len(entries) && len(lines) < msgH; i++ {
		e := entries[i]
		switch e.Kind {
		case "thinking":
			lines = append(lines, dim.Render(fmt.Sprintf("  💭 %s", trunc(e.Thinking, innerW-4))))
		case "tool":
			ic := toolIcon(e.Tool.Name)
			status := green.Render(" ✓")
			if e.Tool.Error != "" { status = red.Render(" ✗ " + e.Tool.Error) }
			if e.Tool.Status == "blocked" { status = dim.Render(" ⛔") }
			if e.Tool.Status == "running" { status = cyan.Render(" …") }
			preview := ""
			if out := strings.TrimSpace(e.Tool.Output); out != "" {
				first := strings.SplitN(out, "\n", 2)[0]
				if len(first) > 40 { first = first[:37] + "…" }
				preview = dim.Render("  " + first)
			}
			step := ""
			if e.Tool.Step > 0 { step = fmt.Sprintf("%2d. ", e.Tool.Step) }
			lines = append(lines, fmt.Sprintf("  %s%s %s%s%s",
				step, ic, e.Tool.Name, status, preview))
		case "divider":
			lines = append(lines, dim.Render("  ── " + e.Content))
		default: // text
			wrapped := wordWrap(e.Content, innerW-2)
			for _, l := range strings.Split(wrapped, "\n") {
				lines = append(lines, "  "+l)
			}
		}
	}

	content := strings.Join(lines, "\n")
	// Show input line at bottom when chat has focus
	if m.focusPanel == 0 {
		prompt := dim.Render("▸ " + m.input.String())
		if m.input.Len() > 0 {
			content = content + "\n" + prompt
		} else {
			content = content + "\n" + prompt
		}
	} else {
		content = content + "\n" + dim.Render("▸ ...")
	}
	return style.Width(w).Height(h).Render(content)
}

func (m *Model) renderPlan(w, h int) string {
	style := panelStyle
	if m.focusPanel == 1 {
		style = panelActiveStyle
	}

	plan := m.plan.plan
	if plan == nil {
		return style.Width(w).Height(h).Render(dim.Render("  no plan"))
	}

	innerW := w - 4
	var lines []string
	lines = append(lines, bold.Render("  "+plan.Title))
	if plan.Description != "" {
		lines = append(lines, dim.Render("  "+plan.Description))
	}
	lines = append(lines, "")

	for _, s := range plan.Steps {
		status := "○"
		switch s.Status {
		case "done": status = green.Render("✓")
		case "running": status = cyan.Render("◉")
		case "error": status = red.Render("✗")
		}
		lines = append(lines, fmt.Sprintf("  %s %d. %s", status, s.Number, trunc(s.Description, innerW-6)))
	}

	if plan.Approved {
		lines = append(lines, "", green.Render("  ✓ APPROVED — executing"))
	} else if m.focusPanel == 1 {
		lines = append(lines, "", accent.Render("  ↵ approve  |  ⌫ dismiss"))
	}

	content := strings.Join(lines, "\n")
	return style.Width(w).Height(h).Render(content)
}

func (m *Model) renderDiff(w, h int) string {
	style := panelStyle
	if m.focusPanel == 2 {
		style = panelActiveStyle
	}

	d := m.diff.diff
	if d == nil {
		return style.Width(w).Height(h).Render(dim.Render("  no diff"))
	}

	innerW := w - 4
	if innerW < 10 { innerW = 10 }

	var lines []string
	lines = append(lines, bold.Render("  "+d.FilePath))

	oldLines := strings.Split(d.OldText, "\n")
	newLines := strings.Split(d.NewText, "\n")
	diffLines := computeLCSDiff(oldLines, newLines)

	shown := 0
	maxLines := h - 4
	for _, dl := range diffLines {
		if shown >= maxLines { break }
		prefix := "  "
		switch dl.Kind {
		case "+":
			prefix = green.Render("+ ")
		case "-":
			prefix = red.Render("- ")
		case "@":
			prefix = cyan.Render("… ")
		}
		line := prefix + trunc(dl.Text, innerW-4)
		lines = append(lines, line)
		shown++
	}

	content := strings.Join(lines, "\n")
	return style.Width(w).Height(h).Render(content)
}

func (m *Model) renderStatus(w int) string {
	s := m.status.status
	if s.Model == "" {
		return statusStyle.Width(w).Render("  LUMEN  ·  loading…")
	}

	pct := 0
	if s.TokensIn > 0 {
		pct = int(float64(s.CacheHit) / float64(s.TokensIn) * 100)
	}

	modeStyle := statusModeDefault
	icon := "🛡"
	switch s.Mode {
	case "bypass": modeStyle = statusModeBypass; icon = "🔓"
	case "plan": modeStyle = statusModePlan; icon = "🔒"
	case "accept-edits": modeStyle = statusModeAccept; icon = "✅"
	}

	modePart := modeStyle.Render(" " + icon + " " + s.Mode + " ")
	modelPart := fmt.Sprintf("%s/%s", s.Provider, s.Model)
	tokensPart := fmt.Sprintf("📊 %.0fk", float64(s.TokensIn+s.TokensOut)/1000)
	cachePart := fmt.Sprintf("♻ %d%%", pct)
	costPart := fmt.Sprintf("💰 $%.4f", s.Cost)
	stepPart := fmt.Sprintf("⚙ %dst · #%d", s.Steps, s.Turns)

	if s.State == "thinking" {
		spinners := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		stepPart = spinners[m.spinnerTick%len(spinners)] + " thinking…"
	}

	left := lipgloss.JoinHorizontal(lipgloss.Center, modePart, " ", dim.Render(modelPart))
	right := lipgloss.JoinHorizontal(lipgloss.Center,
		cyan.Render(tokensPart), " ",
		green.Render(cachePart), " ",
		lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Render(costPart), " ",
		lipgloss.NewStyle().Foreground(lipgloss.Color("5")).Render(stepPart),
	)

	leftW := lipgloss.Width(left)
	rightW := lipgloss.Width(right)
	gap := w - leftW - rightW - 2
	if gap < 1 { gap = 1 }

	return statusStyle.Width(w).Render(left + strings.Repeat(" ", gap) + right)
}

// ── Helpers ────────────────────────────────────────────────

func trunc(s string, n int) string {
	if n <= 0 { return "" }
	runes := []rune(s)
	if len(runes) <= n { return s }
	return string(runes[:n-1]) + "…"
}

func wordWrap(text string, width int) string {
	if width <= 0 { return text }
	var sb strings.Builder
	for _, paragraph := range strings.Split(text, "\n") {
		if len(paragraph) <= width {
			sb.WriteString(paragraph + "\n")
			continue
		}
		for len(paragraph) > width {
			split := width
			// Try to break at space
			idx := strings.LastIndexByte(paragraph[:width+1], ' ')
			if idx > 0 { split = idx }
			sb.WriteString(paragraph[:split] + "\n")
			paragraph = strings.TrimSpace(paragraph[split:])
		}
		if len(paragraph) > 0 {
			sb.WriteString(paragraph + "\n")
		}
	}
	return strings.TrimSuffix(sb.String(), "\n")
}

// DiffLine represents one line in a computed diff.
type DiffLine struct {
	Kind  string // "+", "-", " ", "@"
	Text  string
	OldNo int
	NewNo int
}

func computeLCSDiff(oldLines, newLines []string) []DiffLine {
	var result []DiffLine

	oi, ni := 0, 0
	for oi < len(oldLines) || ni < len(newLines) {
		if oi < len(oldLines) && ni < len(newLines) && oldLines[oi] == newLines[ni] {
			result = append(result, DiffLine{" ", oldLines[oi], oi + 1, ni + 1})
			oi++; ni++
		} else if ni < len(newLines) && (oi >= len(oldLines) || !containsLine(oldLines, oi, newLines[ni])) {
			result = append(result, DiffLine{"+", newLines[ni], 0, ni + 1})
			ni++
		} else if oi < len(oldLines) {
			result = append(result, DiffLine{"-", oldLines[oi], oi + 1, 0})
			oi++
		} else {
			break
		}
	}
	return result
}

func containsLine(lines []string, start int, target string) bool {
	for i := start; i < len(lines); i++ {
		if lines[i] == target { return true }
	}
	return false
}

// ── Public API ─────────────────────────────────────────────

// RunTUI starts the Bubble Tea TUI program.
// It blocks until the user quits (Ctrl+C or Esc).
func RunTUI(model *Model) error {
	p := tea.NewProgram(model, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

// WaitInput blocks until the user submits a line in the TUI chat panel.
// Returns the line text, or empty string if the TUI quit.
func (m *Model) WaitInput() string {
	m.mu.Lock()
	if m.inputCh == nil {
		m.inputCh = make(chan string, 8)
	}
	ch := m.inputCh
	m.mu.Unlock()
	return <-ch
}
