// Package tui provides a Bubble Tea terminal UI for Lumen.
// Sub-packages handle diff rendering, plan approval, and thinking-block
// folding. The main tui.go orchestrates the three-panel layout.
package tui

import (
	"context"
	"fmt"
	"strings"
	"time"

	"lumen/internal/agent"
	"lumen/internal/control"
	"lumen/internal/event"
	"lumen/internal/timeline"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ── Top-level Model ────────────────────────────────────────

// Model is the Bubble Tea application model.
type Model struct {
	ctrl    *control.Controller
	ctx     context.Context
	cancel  context.CancelFunc

	chat     *ChatModel
	status   *StatusModel
	input    *InputModel
	diff     *DiffPanel
	plan     *PlanPanel
	thinking *ThinkingPanel

	width    int
	height   int
	ready    bool
	running  bool
	quitting bool

	eventCh chan event.Event
}

// New constructs a TUI model connected to a controller.
func New(ctrl *control.Controller) *Model {
	ctx, cancel := context.WithCancel(context.Background())
	return &Model{
		ctrl:     ctrl,
		ctx:      ctx,
		cancel:   cancel,
		chat:     NewChatModel(),
		status:   NewStatusModel(ctrl),
		input:    NewInputModel(),
		diff:     NewDiffPanel(),
		plan:     NewPlanPanel(),
		thinking: NewThinkingPanel(),
		eventCh:  make(chan event.Event, 256),
	}
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(tea.EnterAltScreen, waitForEvents(m.eventCh))
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ready = true
		m.chat.SetSize(msg.Width, msg.Height-8)
		m.input.SetWidth(msg.Width)
		m.status.SetWidth(msg.Width)
		m.diff.SetSize(msg.Width)
		m.plan.SetSize(msg.Width, msg.Height)
		return m, nil

	case tea.KeyMsg:
		if m.plan.Active() {
			return m, m.plan.HandleKey(msg)
		}
		if m.quitting {
			return m, tea.Quit
		}
		switch msg.String() {
		case "ctrl+c":
			m.quitting = true
			m.cancel()
			m.ctrl.Close()
			return m, tea.Quit
		case "ctrl+r":
			if rewound, err := m.ctrl.Rewind(); err == nil && len(rewound) > 0 {
				m.chat.AddNotice("rewound " + strings.Join(rewound, ", "))
			}
			return m, nil
		case "ctrl+d":
			m.diff.Toggle()
			return m, nil
		case "enter":
			if m.input.text != "" && !m.running {
				text := m.input.text
				m.input.Reset()
				m.chat.AddUser(text)
				go m.runPrompt(text)
				return m, nil
			}
		case "backspace":
			m.input.Backspace()
			return m, nil
		default:
			if !m.running && len(msg.String()) == 1 {
				m.input.Append(msg.String())
			}
		}

	case eventMsg:
		m.handleEvent(msg.event)
		return m, waitForEvents(m.eventCh)

	case doneMsg:
		m.running = false
		if msg.err != nil {
			m.chat.AddNotice("error: " + msg.err.Error())
		}
		m.input.Focus()
		return m, waitForEvents(m.eventCh)
	}

	return m, nil
}

func (m *Model) View() string {
	if !m.ready {
		return "Lumen — initializing...\n"
	}
	if m.quitting {
		return "Lumen — 再见\n"
	}
	if m.plan.Active() {
		return m.plan.View()
	}

	chat := m.chat.View()
	status := m.status.View()
	input := m.input.View()

	if m.diff.Active() {
		diff := m.diff.View()
		return lipgloss.JoinVertical(lipgloss.Left, chat, status, diff, input)
	}
	return lipgloss.JoinVertical(lipgloss.Left, chat, status, input)
}

// ── Event handling ────────────────────────────────────────

func (m *Model) handleEvent(ev event.Event) {
	switch ev.Kind {
	case event.Phase:
		m.status.SetPhase(ev.Text)
	case event.Text:
		m.chat.AppendText(ev.Text)
	case event.Reasoning:
		m.thinking.Append(ev.Text)
		m.chat.AppendReasoning(ev.Text)
	case event.ToolDispatch:
		m.chat.AddToolCall(ev.Tool)
	case event.ToolResult:
		m.chat.AddToolResult(ev.Tool)
		if ev.Tool.Err == "" && !ev.Tool.Blocked && isWriterTool(ev.Tool.Name) {
			m.diff.RecordChange(ev.Tool)
		}
	case event.UsageKind:
		if ev.Usage != nil {
			m.status.UpdateUsage(ev.Usage)
		}
	case event.Notice:
		m.chat.AddNotice(ev.Text)
	case event.TurnStarted:
		m.chat.StartTurn()
	case event.TurnDone:
	case event.PlanApproval:
		m.plan.Show(ev.Text)
	}
}

func isWriterTool(name string) bool {
	return name == "write_file" || name == "edit_file" || name == "multi_edit"
}

// ── Prompt execution ──────────────────────────────────────

func (m *Model) runPrompt(prompt string) {
	m.running = true
	m.thinking.Reset()

	if strings.HasPrefix(prompt, "/") {
		m.runSlashCommand(prompt)
		m.running = false
		return
	}

	sink := newTuiSink(m.eventCh)
	m.ctrl.Agent().SetSink(sink)

	go func() {
		err := m.ctrl.Run(m.ctx, prompt)
		sink.ch <- event.Event{Kind: event.TurnDone}
		go func() { sink.ch <- event.Event{Kind: event.TurnDone} }()
		_ = err
	}()
}

func (m *Model) runSlashCommand(cmd string) {
	parts := strings.Fields(strings.TrimPrefix(cmd, "/"))
	if len(parts) == 0 {
		return
	}
	switch strings.ToLower(parts[0]) {
	case "help", "?":
		m.chat.AddNotice("/status /cost /cache /rewind /replay /changes /help /diff /plan")
	case "status":
		m.chat.AddNotice(fmt.Sprintf("%s/%s · %s mode · permissions: %s",
			m.ctrl.ProviderName(), m.ctrl.ModelName(), "running", m.ctrl.PermissionMode()))
	case "cost":
		hit, miss := m.ctrl.Agent().SessionCache()
		rate := 0.0
		if hit+miss > 0 {
			rate = float64(hit) / float64(hit+miss) * 100
		}
		m.chat.AddNotice(fmt.Sprintf("cache: %.0f%% hit (%d/%d tokens)", rate, hit, hit+miss))
	case "cache":
		reasons := m.ctrl.Agent().CacheReasons()
		m.chat.AddNotice(fmt.Sprintf("cache churn events: %d", len(reasons)))
		for _, r := range reasons {
			m.chat.AddNotice("  · " + r)
		}
	case "rewind":
		if rewound, err := m.ctrl.Rewind(); err == nil {
			m.chat.AddNotice("rewound " + strings.Join(rewound, ", "))
		} else {
			m.chat.AddNotice("rewind: " + err.Error())
		}
	case "replay":
		entries, err := timeline.LoadTimeline(".lumen/timeline.jsonl")
		if err != nil {
			m.chat.AddNotice("no timeline: " + err.Error())
		} else {
			m.chat.AddNotice(timeline.FormatTimeline(entries))
		}
	case "changes":
		changes, err := timeline.LoadChanges(".lumen/timeline.jsonl")
		if err != nil {
			m.chat.AddNotice("no changes: " + err.Error())
		} else {
			m.chat.AddNotice(timeline.FormatChanges(changes))
		}
	case "diff":
		m.diff.Toggle()
	case "plan":
		m.plan.Show("Plan mode active — enter your plan or press Esc")
	default:
		m.chat.AddNotice("unknown: /" + parts[0])
	}
}

// ── Asker implementation ──────────────────────────────────

func (m *Model) Ask(ctx context.Context, questions []event.AskQuestion) ([]event.AskAnswer, error) {
	answers := make([]event.AskAnswer, len(questions))
	for i, q := range questions {
		answers[i] = event.AskAnswer{Header: q.Header}
		if len(q.Options) > 0 {
			answers[i].Answers = []string{q.Options[0].Label}
		}
	}
	return answers, nil
}

var _ agent.Asker = (*Model)(nil)

// ── Custom messages ────────────────────────────────────────

type eventMsg struct{ event event.Event }
type doneMsg struct{ err error }

// ── Event sink bridge ──────────────────────────────────────

type tuiSink struct{ ch chan event.Event }

func newTuiSink(ch chan event.Event) *tuiSink { return &tuiSink{ch: ch} }
func (s *tuiSink) Emit(e event.Event)         { s.ch <- e }

func waitForEvents(ch <-chan event.Event) tea.Cmd {
	return func() tea.Msg {
		e, ok := <-ch
		if !ok {
			return nil
		}
		if e.Kind == event.TurnDone {
			return doneMsg{}
		}
		return eventMsg{event: e}
	}
}

var _ event.Sink = (*tuiSink)(nil)

// ── Chat model ────────────────────────────────────────────

type chatLine struct {
	kind      string
	text      string
	tool      event.Tool
	thinking  bool
	timestamp time.Time
}

type ChatModel struct {
	lines  []chatLine
	width  int
	height int
}

func NewChatModel() *ChatModel { return &ChatModel{} }

func (c *ChatModel) SetSize(w, h int) {
	c.width = w
	if h < 5 {
		h = 5
	}
	c.height = h
}

func (c *ChatModel) AddUser(text string) {
	c.lines = append(c.lines, chatLine{kind: "user", text: text, timestamp: time.Now()})
}
func (c *ChatModel) AddNotice(text string) {
	c.lines = append(c.lines, chatLine{kind: "notice", text: text, timestamp: time.Now()})
}
func (c *ChatModel) AddToolCall(t event.Tool) {
	c.lines = append(c.lines, chatLine{kind: "tool", text: "⚙ " + t.Name, tool: t, timestamp: time.Now()})
}
func (c *ChatModel) AddToolResult(t event.Tool) {
	icon := "✓"
	if t.Err != "" {
		icon = "✗"
	} else if t.Blocked {
		icon = "⊘"
	}
	c.lines = append(c.lines, chatLine{kind: "tool", text: fmt.Sprintf("  %s %s", icon, t.Name), tool: t, timestamp: time.Now()})
}

func (c *ChatModel) StartTurn() {}
func (c *ChatModel) AddPlan(text string) {
	c.lines = append(c.lines, chatLine{kind: "plan", text: text, timestamp: time.Now()})
}

func (c *ChatModel) AppendText(text string) {
	n := len(c.lines)
	if n > 0 && c.lines[n-1].kind == "assistant" {
		c.lines[n-1].text += text
	} else {
		c.lines = append(c.lines, chatLine{kind: "assistant", text: text, timestamp: time.Now()})
	}
}

func (c *ChatModel) AppendReasoning(text string) {
	n := len(c.lines)
	if n > 0 && c.lines[n-1].kind == "reasoning" {
		c.lines[n-1].text += text
	} else {
		c.lines = append(c.lines, chatLine{kind: "reasoning", text: text, thinking: true, timestamp: time.Now()})
	}
}

func (c *ChatModel) View() string {
	var sb strings.Builder
	start := len(c.lines) - c.height
	if start < 0 {
		start = 0
	}
	for i := start; i < len(c.lines); i++ {
		line := c.lines[i]
		switch line.kind {
		case "user":
			sb.WriteString(userStyle.Render("▸ " + line.text))
		case "assistant":
			sb.WriteString(assistantStyle.Render(line.text))
		case "reasoning":
			sb.WriteString(reasoningStyle.Render("💭 " + line.text))
		case "tool":
			sb.WriteString(toolStyle.Render(line.text))
		case "notice":
			sb.WriteString(noticeStyle.Render("ℹ " + line.text))
		case "plan":
			sb.WriteString(planStyle.Render(line.text))
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}

// ── Status bar ────────────────────────────────────────────

type StatusModel struct {
	ctrl       *control.Controller
	phase      string
	prompt     int
	completion int
	cacheHit   int
	cacheMiss  int
	width      int
}

func NewStatusModel(ctrl *control.Controller) *StatusModel {
	return &StatusModel{ctrl: ctrl, phase: "ready"}
}
func (s *StatusModel) SetWidth(w int) { s.width = w }
func (s *StatusModel) SetPhase(p string) { s.phase = p }

func (s *StatusModel) UpdateUsage(u *event.Usage) {
	s.prompt += u.PromptTokens
	s.completion += u.CompletionTokens
	s.cacheHit += u.CacheHitTokens
	s.cacheMiss += u.CacheMissTokens
}

func (s *StatusModel) View() string {
	cacheRate := 0.0
	total := s.cacheHit + s.cacheMiss
	if total > 0 {
		cacheRate = float64(s.cacheHit) / float64(total) * 100
	}
	left := fmt.Sprintf("%s/%s · %s · %s",
		s.ctrl.ProviderName(), s.ctrl.ModelName(), s.phase, s.ctrl.PermissionMode())
	right := fmt.Sprintf("%dt · cache:%.0f%%", s.prompt+s.completion, cacheRate)
	padding := s.width - lipgloss.Width(left) - lipgloss.Width(right)
	if padding < 1 {
		padding = 1
	}
	return statusBarStyle.Render(left + strings.Repeat(" ", padding) + right)
}

// ── Input model ────────────────────────────────────────────

type InputModel struct {
	text  string
	width int
}

func NewInputModel() *InputModel { return &InputModel{} }
func (i *InputModel) SetWidth(w int) { i.width = w }
func (i *InputModel) Reset() { i.text = "" }
func (i *InputModel) Focus() {}
func (i *InputModel) Backspace() {
	if len(i.text) > 0 {
		i.text = i.text[:len(i.text)-1]
	}
}
func (i *InputModel) Append(s string) { i.text += s }

func (i *InputModel) View() string {
	cursor := "▊"
	if i.text == "" {
		return inputStyle.Render("> " + cursor)
	}
	return inputStyle.Render("> " + i.text + cursor)
}

// ── Diff panel ─────────────────────────────────────────────

type DiffPanel struct {
	active  bool
	changes []event.Tool
}

func NewDiffPanel() *DiffPanel { return &DiffPanel{} }
func (d *DiffPanel) Toggle()   { d.active = !d.active }
func (d *DiffPanel) Active() bool { return d.active }
func (d *DiffPanel) SetSize(w int) {}

func (d *DiffPanel) RecordChange(t event.Tool) {
	d.changes = append(d.changes, t)
}

func (d *DiffPanel) View() string {
	if len(d.changes) == 0 {
		return diffStyle.Render("No file changes yet")
	}
	var sb strings.Builder
	sb.WriteString(diffStyle.Render("── Changed files ──"))
	sb.WriteByte('\n')
	for _, t := range d.changes {
		icon := "✎"
		if t.Err != "" {
			icon = "✗"
		}
		sb.WriteString(fmt.Sprintf("  %s %s", icon, t.Name))
		if t.Args != "" {
			sb.WriteString(" — " + t.Args)
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}

// ── Plan panel ─────────────────────────────────────────────

type PlanPanel struct {
	active bool
	text   string
}

func NewPlanPanel() *PlanPanel { return &PlanPanel{} }
func (p *PlanPanel) Active() bool { return p.active }
func (p *PlanPanel) Show(text string) { p.active = true; p.text = text }
func (p *PlanPanel) SetSize(w, h int) {}

func (p *PlanPanel) HandleKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "y", "Y":
		p.active = false
		return func() tea.Msg { return eventMsg{event: event.Event{Kind: event.Notice, Text: "plan approved"}} }
	case "n", "N", "esc":
		p.active = false
		return nil
	}
	return nil
}

func (p *PlanPanel) View() string {
	var sb strings.Builder
	sb.WriteString(approvalStyle.Render("📋 Plan Approval"))
	sb.WriteString("\n\n")
	sb.WriteString(p.text)
	sb.WriteString("\n\n")
	sb.WriteString("  [Y] approve  [N] deny  [Esc] dismiss")
	return sb.String()
}

// ── Thinking panel ─────────────────────────────────────────

type ThinkingPanel struct {
	buf   strings.Builder
	folded bool
}

func NewThinkingPanel() *ThinkingPanel { return &ThinkingPanel{folded: true} }
func (t *ThinkingPanel) Append(text string) { t.buf.WriteString(text) }
func (t *ThinkingPanel) Reset()             { t.buf.Reset(); t.folded = true }
func (t *ThinkingPanel) Toggle()            { t.folded = !t.folded }

func (t *ThinkingPanel) View() string {
	if t.buf.Len() == 0 {
		return ""
	}
	if t.folded {
		return thinkingFoldedStyle.Render("💭 thinking... (click to expand)")
	}
	return thinkingExpandedStyle.Render("💭 " + t.buf.String())
}

// ── Styles ─────────────────────────────────────────────────

var (
	userStyle            = lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Bold(true)
	assistantStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("15"))
	reasoningStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Italic(true)
	toolStyle            = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	noticeStyle          = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	planStyle            = lipgloss.NewStyle().Foreground(lipgloss.Color("13"))
	statusBarStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(lipgloss.Color("7")).Bold(true)
	inputStyle           = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	diffStyle            = lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Bold(true)
	approvalStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true).Border(lipgloss.NormalBorder())
	thinkingFoldedStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Italic(true)
	thinkingExpandedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Italic(true)
)

// ── Public entry point ─────────────────────────────────────

// RunTUI starts the interactive terminal session.
func RunTUI(ctrl *control.Controller) error {
	ctrl.SetAsker(New(ctrl))
	p := tea.NewProgram(New(ctrl), tea.WithAltScreen())
	_, err := p.Run()
	return err
}
