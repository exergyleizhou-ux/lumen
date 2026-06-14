// Package tui provides a Bubble Tea terminal UI for Lumen.
package tui

import (
	"context"
	"fmt"
	"strings"

	"lumen/internal/agent"
	"lumen/internal/control"
	"lumen/internal/event"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ── Model ─────────────────────────────────────────────────

type Model struct {
	ctrl   *control.Controller
	ctx    context.Context
	cancel context.CancelFunc

	chat     chatModel
	status   statusModel
	approval *approvalModel
	input    inputModel

	width  int
	height int
	ready  bool

	running  bool
	quitting bool

	// Event channel from agent goroutine
	eventCh chan event.Event
}

func New(ctrl *control.Controller) *Model {
	ctx, cancel := context.WithCancel(context.Background())
	return &Model{
		ctrl:    ctrl,
		ctx:     ctx,
		cancel:  cancel,
		chat:    newChatModel(),
		status:  newStatusModel(ctrl),
		input:   newInputModel(),
		eventCh: make(chan event.Event, 256),
	}
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(
		tea.EnterAltScreen,
		waitForEvents(m.eventCh),
	)
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ready = true
		m.chat.setSize(msg.Width, msg.Height-5)
		m.input.setWidth(msg.Width)
		m.status.setWidth(msg.Width)
		return m, nil

	case tea.KeyMsg:
		// Dismiss approval first
		if m.approval != nil {
			switch msg.String() {
			case "y", "Y":
				m.approval.approve()
				m.approval = nil
				return m, nil
			case "n", "N", "esc":
				m.approval.deny()
				m.approval = nil
				return m, nil
			}
			return m, nil
		}

		switch msg.String() {
		case "ctrl+c":
			m.quitting = true
			m.cancel()
			return m, tea.Quit
		case "ctrl+r":
			if rewound, err := m.ctrl.Rewind(); err == nil && len(rewound) > 0 {
				m.chat.addNotice("rewound " + strings.Join(rewound, ", "))
			}
			return m, nil
		case "enter":
			if m.input.text != "" && !m.running {
				text := m.input.text
				m.input.reset()
				go m.runPrompt(text)
				return m, nil
			}
		case "backspace":
			m.input.backspace()
			return m, nil
		default:
			if !m.running && len(msg.String()) == 1 {
				m.input.append(msg.String())
			}
		}
		return m, nil

	case eventMsg:
		m.handleEvent(msg.event)
		return m, waitForEvents(m.eventCh)

	case doneMsg:
		m.running = false
		if msg.err != nil {
			m.chat.addNotice("error: " + msg.err.Error())
		}
		return m, waitForEvents(m.eventCh)
	}

	return m, nil
}

func (m *Model) View() string {
	if !m.ready {
		return "initializing...\n"
	}
	if m.quitting {
		return "Lumen — 再见\n"
	}
	if m.approval != nil {
		return m.approval.View()
	}

	chat := m.chat.View()
	status := m.status.View()
	input := m.input.View()

	return lipgloss.JoinVertical(lipgloss.Left, chat, status, input)
}

// ── Prompt execution ──────────────────────────────────────

func (m *Model) runPrompt(prompt string) {
	m.running = true
	m.chat.addUser(prompt)

	// Check slash commands
	if strings.HasPrefix(prompt, "/") {
		m.runSlashCommand(prompt)
		m.running = false
		return
	}

	sink := newTuiSink(m.eventCh)
	ag := m.ctrl.Agent()
	ag.SetSink(sink)

	go func() {
		err := m.ctrl.Run(m.ctx, prompt)
		sink.ch <- event.Event{Kind: event.TurnDone}
		// Signal done via channel
		go func() { sink.ch <- event.Event{Kind: event.TurnDone} }()
		_ = err
	}()
}

func (m *Model) runSlashCommand(cmd string) {
	cmd = strings.TrimPrefix(cmd, "/")
	switch strings.ToLower(cmd) {
	case "help", "?":
		m.chat.addNotice("Slash commands: /status /cost /cache /rewind /skills /help")
	case "status":
		m.chat.addNotice(fmt.Sprintf("%s/%s — %s mode",
			m.ctrl.ProviderName(), m.ctrl.ModelName(), m.ctrl.PermissionMode()))
	case "rewind":
		if rewound, err := m.ctrl.Rewind(); err == nil {
			m.chat.addNotice("Rewound " + strings.Join(rewound, ", "))
		} else {
			m.chat.addNotice("Rewind: " + err.Error())
		}
	default:
		m.chat.addNotice("Unknown command: /" + cmd)
	}
}

func (m *Model) handleEvent(ev event.Event) {
	switch ev.Kind {
	case event.Phase:
		m.status.setPhase(ev.Text)
	case event.Text:
		m.chat.appendText(ev.Text)
	case event.Reasoning:
		m.chat.appendReasoning(ev.Text)
	case event.ToolDispatch:
		m.chat.addToolCall(ev.Tool)
	case event.ToolResult:
		m.chat.addToolResult(ev.Tool)
	case event.UsageKind:
		if ev.Usage != nil {
			m.status.updateUsage(ev.Usage)
		}
	case event.Notice:
		m.chat.addNotice(ev.Text)
	case event.TurnStarted:
		m.chat.startTurn()
	case event.TurnDone:
		// handled by doneMsg
	case event.PlanApproval:
		m.chat.addPlan(ev.Text)
	}
}

// ── Asker ──────────────────────────────────────────────

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

// ── Custom messages ─────────────────────────────────────

type eventMsg struct{ event event.Event }
type doneMsg struct{ err error }

// ── Event sink ──────────────────────────────────────────

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

// ── Chat model ─────────────────────────────────────────

type chatLine struct {
	kind string
	text string
	tool event.Tool
}

type chatModel struct {
	lines  []chatLine
	width  int
	height int
}

func newChatModel() chatModel { return chatModel{} }
func (c *chatModel) setSize(w, h int) {
	c.width = w
	if h < 5 {
		h = 5
	}
	c.height = h
}

func (c *chatModel) addUser(text string) {
	c.lines = append(c.lines, chatLine{kind: "user", text: text})
}
func (c *chatModel) addNotice(text string) {
	c.lines = append(c.lines, chatLine{kind: "notice", text: text})
}
func (c *chatModel) addToolCall(t event.Tool) {
	c.lines = append(c.lines, chatLine{kind: "tool", text: "⚙ " + t.Name, tool: t})
}
func (c *chatModel) addToolResult(t event.Tool) {
	icon := "✓"
	if t.Err != "" {
		icon = "✗"
	} else if t.Blocked {
		icon = "⊘"
	}
	c.lines = append(c.lines, chatLine{kind: "tool", text: fmt.Sprintf("  %s %s", icon, t.Name), tool: t})
}
func (c *chatModel) startTurn() {}
func (c *chatModel) addPlan(text string) {
	c.lines = append(c.lines, chatLine{kind: "plan", text: text})
}

func (c *chatModel) appendText(text string) {
	n := len(c.lines)
	if n > 0 && c.lines[n-1].kind == "assistant" {
		c.lines[n-1].text += text
	} else {
		c.lines = append(c.lines, chatLine{kind: "assistant", text: text})
	}
}
func (c *chatModel) appendReasoning(text string) {
	n := len(c.lines)
	if n > 0 && c.lines[n-1].kind == "reasoning" {
		c.lines[n-1].text += text
	} else {
		c.lines = append(c.lines, chatLine{kind: "reasoning", text: text})
	}
}

func (c *chatModel) View() string {
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
			sb.WriteString(reasoningStyle.Render(line.text))
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

// ── Status bar ─────────────────────────────────────────

type statusModel struct {
	ctrl       *control.Controller
	phase      string
	prompt     int
	completion int
	cacheHit   int
	cacheMiss  int
	width      int
}

func newStatusModel(ctrl *control.Controller) statusModel {
	return statusModel{ctrl: ctrl, phase: "ready"}
}
func (s *statusModel) setWidth(w int)  { s.width = w }
func (s *statusModel) setPhase(p string) { s.phase = p }
func (s *statusModel) updateUsage(u *event.Usage) {
	s.prompt += u.PromptTokens
	s.completion += u.CompletionTokens
	s.cacheHit += u.CacheHitTokens
	s.cacheMiss += u.CacheMissTokens
}

func (s *statusModel) View() string {
	cacheRate := 0.0
	total := s.cacheHit + s.cacheMiss
	if total > 0 {
		cacheRate = float64(s.cacheHit) / float64(total) * 100
	}
	left := fmt.Sprintf("%s/%s · %s", s.ctrl.ProviderName(), s.ctrl.ModelName(), s.phase)
	right := fmt.Sprintf("%dt · cache:%.0f%%", s.prompt+s.completion, cacheRate)
	padding := s.width - lipgloss.Width(left) - lipgloss.Width(right)
	if padding < 1 {
		padding = 1
	}
	return statusStyle.Render(left + strings.Repeat(" ", padding) + right)
}

// ── Input ─────────────────────────────────────────────

type inputModel struct {
	text  string
	width int
}

func newInputModel() inputModel { return inputModel{} }
func (i *inputModel) setWidth(w int) {
	i.width = w
}
func (i *inputModel) reset() { i.text = "" }
func (i *inputModel) backspace() {
	if len(i.text) > 0 {
		i.text = i.text[:len(i.text)-1]
	}
}
func (i *inputModel) append(s string) { i.text += s }

func (i *inputModel) View() string {
	cursor := "▊"
	if i.text == "" {
		return inputStyle.Render("> " + cursor)
	}
	return inputStyle.Render("> " + i.text + cursor)
}

// ── Approval dialog ────────────────────────────────────

type approvalModel struct {
	toolName string
	details  string
	result   chan bool
}

func newApproval(toolName, details string) *approvalModel {
	return &approvalModel{
		toolName: toolName,
		details:  details,
		result:   make(chan bool, 1),
	}
}
func (a *approvalModel) approve() { a.result <- true }
func (a *approvalModel) deny()    { a.result <- false }

func (a *approvalModel) View() string {
	var sb strings.Builder
	sb.WriteString(approvalStyle.Render("⚠  Approve this tool call?"))
	sb.WriteByte('\n')
	sb.WriteString(fmt.Sprintf("  Tool: %s\n", a.toolName))
	sb.WriteString(fmt.Sprintf("  %s\n", a.details))
	sb.WriteByte('\n')
	sb.WriteString("  [Y] approve  [N] deny  [Esc] dismiss")
	return sb.String()
}

// ── Styles ─────────────────────────────────────────────

var (
	userStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("14")).Bold(true)
	assistantStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("15"))
	reasoningStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Italic(true)
	toolStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	noticeStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	planStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("13"))
	statusStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(lipgloss.Color("7")).Bold(true)
	inputStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	approvalStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true).Border(lipgloss.NormalBorder())
)

// ── Public entry point ─────────────────────────────────

func RunTUI(ctrl *control.Controller) error {
	m := New(ctrl)
	ctrl.SetAsker(m)
	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}
