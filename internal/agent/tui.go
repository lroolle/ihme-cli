package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"unicode"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/lroolle/ihme-cli/internal/app"
	"github.com/lroolle/ihme-cli/pkg/agentkit"
)

// The interactive frontend is one Bubble Tea program. It owns stdin for the
// entire session, so model output, free-form questions, and mutation consent
// can never race separate readers or reinterpret type-ahead.

type tuiBridge struct {
	program *tea.Program
}

type promptReply struct {
	answer string
	err    error
}

type promptRequestMsg struct {
	prompt userPrompt
	reply  chan promptReply
}

type agentEventMsg struct{ event agentkit.Event }

type turnDoneMsg struct {
	transcript []agentkit.Message
	err        error
}

func (b *tuiBridge) ask(ctx context.Context, prompt userPrompt) (string, error) {
	reply := make(chan promptReply, 1)
	b.program.Send(promptRequestMsg{prompt: prompt, reply: reply})
	select {
	case got := <-reply:
		return got.answer, got.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (b *tuiBridge) emit(event agentkit.Event) error {
	b.program.Send(agentEventMsg{event: event})
	return nil
}

type tuiPhase uint8

const (
	phaseReady tuiPhase = iota
	phaseRunning
	phaseQuestion
	phaseConsent
)

type stepLevel uint8

const (
	stepOK stepLevel = iota
	stepInfo
	stepWarn
	stepError
)

type tuiStep struct {
	key    string
	text   string
	detail string
	level  stepLevel
}

type tuiStyles struct {
	accent   lipgloss.Style
	muted    lipgloss.Style
	strong   lipgloss.Style
	success  lipgloss.Style
	warning  lipgloss.Style
	danger   lipgloss.Style
	selected lipgloss.Style
	button   lipgloss.Style
}

func newTUIStyles(dark bool) tuiStyles {
	lightDark := lipgloss.LightDark(dark)
	accent := lightDark(lipgloss.Color("#075EA8"), lipgloss.Color("#7AA2F7"))
	muted := lightDark(lipgloss.Color("#57606A"), lipgloss.Color("#8B949E"))
	green := lightDark(lipgloss.Color("#1A7F37"), lipgloss.Color("#3FB950"))
	yellow := lightDark(lipgloss.Color("#9A6700"), lipgloss.Color("#D29922"))
	red := lightDark(lipgloss.Color("#CF222E"), lipgloss.Color("#F85149"))
	selection := lightDark(lipgloss.Color("#DDF4FF"), lipgloss.Color("#263B5C"))
	return tuiStyles{
		accent:   lipgloss.NewStyle().Foreground(accent),
		muted:    lipgloss.NewStyle().Foreground(muted),
		strong:   lipgloss.NewStyle().Bold(true),
		success:  lipgloss.NewStyle().Foreground(green),
		warning:  lipgloss.NewStyle().Foreground(yellow),
		danger:   lipgloss.NewStyle().Foreground(red),
		selected: lipgloss.NewStyle().Bold(true).Foreground(accent).Background(selection).Padding(0, 1),
		button:   lipgloss.NewStyle().Foreground(muted).Padding(0, 1),
	}
}

type tuiModel struct {
	ctx        context.Context
	session    *session
	appleID    string
	grant      GrantMode
	width      int
	dark       bool
	styles     tuiStyles
	input      textinput.Model
	spinner    spinner.Model
	phase      tuiPhase
	welcome    bool
	quitting   bool
	stopping   bool
	transcript []agentkit.Message

	currentUser string
	steps       []tuiStep
	assistant   string
	textTurn    int
	activity    string
	reason      string

	prompt     *promptRequestMsg
	choice     int
	savedDraft string

	history      []string
	historyIndex int
	historyDraft string
	cancelTurn   context.CancelFunc
}

func newTUIModel(ctx context.Context, s *session, appleID string, grant GrantMode) *tuiModel {
	input := textinput.New()
	input.Prompt = "› "
	input.Placeholder = "Ask about your Hide My Email addresses"
	input.CharLimit = 2000
	input.SetWidth(76)
	spin := spinner.New(spinner.WithSpinner(spinner.MiniDot))
	m := &tuiModel{
		ctx:          ctx,
		session:      s,
		appleID:      appleID,
		grant:        grant,
		width:        80,
		dark:         true,
		input:        input,
		spinner:      spin,
		phase:        phaseReady,
		welcome:      true,
		transcript:   startTranscript(),
		historyIndex: 0,
	}
	m.setStyles(true)
	return m
}

func (m *tuiModel) setStyles(dark bool) {
	m.dark = dark
	m.styles = newTUIStyles(dark)
	inputStyles := textinput.DefaultStyles(dark)
	inputStyles.Focused.Prompt = m.styles.accent
	inputStyles.Focused.Placeholder = m.styles.muted
	inputStyles.Blurred.Prompt = m.styles.muted
	inputStyles.Blurred.Placeholder = m.styles.muted
	m.input.SetStyles(inputStyles)
	m.spinner.Style = m.styles.accent
}

func (m *tuiModel) Init() tea.Cmd {
	return tea.Batch(m.input.Focus(), tea.RequestBackgroundColor)
}

func (m *tuiModel) Update(message tea.Msg) (tea.Model, tea.Cmd) { //nolint:gocyclo
	switch msg := message.(type) {
	case tea.BackgroundColorMsg:
		m.setStyles(msg.IsDark())
		return m, nil

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.input.SetWidth(max(12, m.contentWidth()-2))
		return m, nil

	case spinner.TickMsg:
		if m.phase == phaseRunning {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil

	case promptRequestMsg:
		m.savedDraft = m.input.Value()
		m.input.Reset()
		m.prompt = &msg
		m.choice = 1 // destructive confirmation defaults to Deny.
		if msg.prompt.Kind == promptConsent {
			m.phase = phaseConsent
			m.input.Blur()
			return m, nil
		}
		m.phase = phaseQuestion
		return m, m.input.Focus()

	case agentEventMsg:
		m.handleAgentEvent(msg.event)
		return m, nil

	case turnDoneMsg:
		return m.finishTurn(msg)

	case tea.KeyPressMsg:
		if cmd, handled := m.handleKey(msg); handled {
			return m, cmd
		}
	}

	if m.phase != phaseConsent {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(message)
		return m, cmd
	}
	return m, nil
}

func (m *tuiModel) handleKey(msg tea.KeyPressMsg) (tea.Cmd, bool) { //nolint:gocyclo
	key := msg.String()
	if key == "ctrl+c" {
		switch m.phase {
		case phaseReady:
			m.quitting = true
			return tea.Quit, true
		case phaseQuestion, phaseConsent:
			m.answerPrompt("", context.Canceled)
		}
		m.stopTurn()
		return nil, true
	}

	switch m.phase {
	case phaseReady:
		switch key {
		case "enter":
			return m.submit(), true
		case "ctrl+d":
			if m.input.Value() == "" {
				m.quitting = true
				return tea.Quit, true
			}
		case "up":
			m.historyUp()
			return nil, true
		case "down":
			m.historyDown()
			return nil, true
		}

	case phaseRunning:
		if key == "esc" {
			m.stopTurn()
			return nil, true
		}
		// Enter is deliberately inert while a turn is running. Text can
		// be composed for the next turn, but it cannot become an unseen
		// answer to a later question.
		if key == "enter" {
			return nil, true
		}

	case phaseQuestion:
		switch key {
		case "enter":
			answer := strings.TrimSpace(m.input.Value())
			if answer == "" {
				return nil, true
			}
			m.recordQuestion(answer)
			m.answerPrompt(answer, nil)
			return m.resumeAfterPrompt(), true
		case "esc":
			if m.prompt != nil {
				m.steps = append(m.steps, tuiStep{
					key:   fmt.Sprintf("question:%d", len(m.steps)),
					text:  "Skipped · " + m.prompt.prompt.Title,
					level: stepWarn,
				})
			}
			m.answerPrompt("", errors.New("user skipped the question"))
			return m.resumeAfterPrompt(), true
		}

	case phaseConsent:
		switch key {
		case "left", "shift+tab":
			m.choice = (m.choice + 2) % 3
			return nil, true
		case "right", "tab":
			m.choice = (m.choice + 1) % 3
			return nil, true
		case "y":
			m.choice = 0
			return m.confirmChoice(), true
		case "n", "esc":
			m.choice = 1
			return m.confirmChoice(), true
		case "a":
			m.choice = 2
			return m.confirmChoice(), true
		case "enter":
			return m.confirmChoice(), true
		}
	}
	return nil, false
}

func (m *tuiModel) submit() tea.Cmd {
	line := strings.TrimSpace(m.input.Value())
	if line == "" {
		return nil
	}
	if line == "exit" || line == "quit" || line == "q" {
		m.quitting = true
		return tea.Quit
	}

	if len(m.history) == 0 || m.history[len(m.history)-1] != line {
		m.history = append(m.history, line)
	}
	m.historyIndex = len(m.history)
	m.historyDraft = ""
	m.input.Reset()
	m.currentUser = line
	m.steps = nil
	m.assistant = ""
	m.textTurn = 0
	m.reason = ""
	m.activity = "Thinking"
	m.welcome = false
	m.phase = phaseRunning
	m.stopping = false
	m.transcript = append(m.transcript, agentkit.Message{Role: agentkit.RoleUser, Text: line})

	turnCtx, cancel := context.WithCancel(m.ctx)
	m.cancelTurn = cancel
	transcript := append([]agentkit.Message(nil), m.transcript...)
	run := func() tea.Msg {
		updated, err := m.session.exec(turnCtx, transcript)
		return turnDoneMsg{transcript: updated, err: err}
	}
	return tea.Batch(run, m.spinner.Tick)
}

func (m *tuiModel) stopTurn() {
	if m.cancelTurn != nil && !m.stopping {
		m.stopping = true
		m.activity = "Stopping"
		m.cancelTurn()
	}
}

func (m *tuiModel) answerPrompt(answer string, err error) {
	if m.prompt == nil {
		return
	}
	select {
	case m.prompt.reply <- promptReply{answer: answer, err: err}:
	default:
	}
}

func (m *tuiModel) resumeAfterPrompt() tea.Cmd {
	m.prompt = nil
	m.input.SetValue(m.savedDraft)
	m.savedDraft = ""
	m.phase = phaseRunning
	m.activity = "Thinking"
	return tea.Batch(m.input.Focus(), m.spinner.Tick)
}

func (m *tuiModel) confirmChoice() tea.Cmd {
	answers := []string{"y", "n", "a"}
	labels := []string{"Allowed once", "Denied", "Always allowed this run"}
	level := stepOK
	if m.choice == 1 {
		level = stepWarn
	}
	title := "Action"
	if m.prompt != nil {
		title = m.prompt.prompt.Title
	}
	m.steps = append(m.steps, tuiStep{
		key:   fmt.Sprintf("consent:%d", len(m.steps)),
		text:  labels[m.choice] + " · " + title,
		level: level,
	})
	m.answerPrompt(answers[m.choice], nil)
	return m.resumeAfterPrompt()
}

func (m *tuiModel) recordQuestion(answer string) {
	if m.prompt == nil {
		return
	}
	m.steps = append(m.steps, tuiStep{
		key:    fmt.Sprintf("question:%d", len(m.steps)),
		text:   m.prompt.prompt.Title,
		detail: answer,
		level:  stepInfo,
	})
}

func (m *tuiModel) finishTurn(done turnDoneMsg) (tea.Model, tea.Cmd) {
	if m.cancelTurn != nil {
		m.cancelTurn()
		m.cancelTurn = nil
	}
	m.transcript = done.transcript
	m.activity = ""
	if m.prompt != nil {
		m.answerPrompt("", context.Canceled)
		m.prompt = nil
		m.input.SetValue(m.savedDraft)
		m.savedDraft = ""
	}
	if done.err != nil {
		text := "Turn ended · " + shortError(done.err)
		level := stepError
		if errors.Is(done.err, context.Canceled) && m.stopping {
			text = "Stopped"
			level = stepWarn
		}
		m.steps = append(m.steps, tuiStep{key: "turn:error", text: text, level: level})
	}

	block := strings.TrimRight(m.renderTurn(), "\n") + "\n"
	m.currentUser = ""
	m.steps = nil
	m.assistant = ""
	m.textTurn = 0
	m.phase = phaseReady
	m.stopping = false
	m.historyIndex = len(m.history)
	m.historyDraft = m.input.Value()
	focus := m.input.Focus()
	return m, tea.Sequence(tea.Println(block), focus)
}

func (m *tuiModel) handleAgentEvent(event agentkit.Event) {
	switch e := event.(type) {
	case agentkit.RunStart:
		m.activity = "Thinking"
	case agentkit.TurnStart:
		if m.activity == "" {
			m.activity = "Thinking"
		}
	case agentkit.ModelEvent:
		switch e.Stream.Type {
		case agentkit.StreamThinking:
			// The live status line carries the tail of the model's
			// reasoning summary — visible progress while it works.
			// The finished transcript never includes it: finishTurn
			// clears the activity line before printing the block.
			m.reason += e.Stream.Text
			m.activity = thinkingActivity(m.reason)
		case agentkit.StreamText:
			if e.Stream.Text != "" {
				if m.textTurn != 0 && m.textTurn != e.Turn && m.assistant != "" {
					m.assistant += "\n"
				}
				m.textTurn = e.Turn
				m.assistant += e.Stream.Text
				m.activity = ""
			}
		case agentkit.StreamToolCall:
			m.activity = "Preparing next step"
		}
	case agentkit.ToolStart:
		m.activity = toolActivity(e.Call)
	case agentkit.ToolEnd:
		m.activity = "Thinking"
		if step, ok := toolStep(e); ok {
			m.upsertStep(step)
		}
	case agentkit.RunEnd:
		m.activity = ""
	}
}

func (m *tuiModel) upsertStep(step tuiStep) {
	for i := range m.steps {
		if m.steps[i].key == step.key {
			m.steps[i] = step
			return
		}
	}
	m.steps = append(m.steps, step)
}

func toolActivity(call agentkit.ToolCall) string {
	var args map[string]any
	_ = json.Unmarshal(call.Args, &args)
	quoted := func(key string) string {
		value, _ := args[key].(string)
		if value == "" {
			return ""
		}
		return fmt.Sprintf(" %q", value)
	}
	switch call.Name {
	case "auth_status":
		return "Checking iCloud"
	case "search_addresses":
		return "Searching" + quoted("query")
	case "generate_candidates":
		return "Generating address ideas"
	case "reserve_address":
		return "Reserving" + quoted("address")
	case "deactivate_address":
		return "Deactivating" + quoted("ref")
	case "edit_note":
		return "Updating" + quoted("ref")
	case "ask_user":
		return "Waiting for your answer"
	default:
		return "Working"
	}
}

func toolStep(event agentkit.ToolEnd) (tuiStep, bool) { //nolint:gocyclo
	if event.Call.Name == "ask_user" {
		return tuiStep{}, false // rendered as a question and answer pair.
	}
	if event.Denied {
		return tuiStep{}, false // the consent choice already explains this.
	}
	if event.Err != "" {
		return tuiStep{
			key:   "tool:" + event.Call.Name,
			text:  toolName(event.Call.Name) + " failed · " + shortError(errors.New(event.Err)),
			level: stepError,
		}, true
	}

	step := tuiStep{key: "tool:" + event.Call.Name, level: stepOK}
	switch event.Call.Name {
	case "auth_status":
		step.text = "iCloud connected"

	case "search_addresses":
		var args struct {
			Query string `json:"query"`
		}
		var result struct {
			Count int `json:"count"`
		}
		_ = json.Unmarshal(event.Call.Args, &args)
		_ = json.Unmarshal(event.Result, &result)
		if result.Count == 0 {
			step.text = fmt.Sprintf("No addresses found for %q", args.Query)
		} else {
			step.text = fmt.Sprintf("Found %d address%s for %q", result.Count, plural(result.Count), args.Query)
		}

	case "generate_candidates":
		var result struct {
			Round      int `json:"round"`
			RoundsLeft int `json:"roundsLeft"`
		}
		_ = json.Unmarshal(event.Result, &result)
		if result.RoundsLeft == 0 {
			step.text = fmt.Sprintf("Reviewed %d rounds of address ideas", result.Round)
		} else {
			step.text = fmt.Sprintf("Reviewed address ideas · round %d", result.Round)
		}

	case "reserve_address":
		var args struct {
			Rationale string `json:"rationale"`
		}
		var result struct {
			Address addressView `json:"address"`
			Copied  bool        `json:"copiedToClipboard"`
		}
		_ = json.Unmarshal(event.Call.Args, &args)
		_ = json.Unmarshal(event.Result, &result)
		step.text = "Reserved " + result.Address.Hme
		if result.Copied {
			step.text += " · copied"
		}
		step.detail = "Label: " + result.Address.Label
		// The taste verdict is the product — it belongs in the
		// transcript, not buried in tool-call JSON.
		if why := strings.TrimSpace(args.Rationale); why != "" {
			step.detail += " — " + why
		}

	case "deactivate_address":
		var result struct {
			Status string `json:"status"`
			Hme    string `json:"hme"`
		}
		_ = json.Unmarshal(event.Result, &result)
		if result.Status == "already_inactive" {
			step.text = result.Hme + " was already inactive"
		} else {
			step.text = "Deactivated " + result.Hme
		}

	case "edit_note":
		var result struct {
			Hme string `json:"hme"`
		}
		_ = json.Unmarshal(event.Result, &result)
		step.text = "Updated " + result.Hme

	default:
		step.text = toolName(event.Call.Name) + " complete"
	}
	return step, true
}

func toolName(name string) string {
	name = strings.ReplaceAll(name, "_", " ")
	if name == "" {
		return "Action"
	}
	return strings.ToUpper(name[:1]) + name[1:]
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "es"
}

// thinkingActivity turns an accumulated reasoning summary into a
// one-line status: the most recent line, tail-truncated. Enough to
// see where the model's head is without dumping the whole stream.
func thinkingActivity(reason string) string {
	lines := strings.Split(safeText(reason), "\n")
	last := ""
	for i := len(lines) - 1; i >= 0 && last == ""; i-- {
		last = strings.Join(strings.Fields(lines[i]), " ")
	}
	if last == "" {
		return "Thinking"
	}
	const limit = 64
	if runes := []rune(last); len(runes) > limit {
		last = "…" + string(runes[len(runes)-limit:])
	}
	return "Thinking · " + last
}

func shortError(err error) string {
	text := strings.Join(strings.Fields(safeText(err.Error())), " ")
	const limit = 180
	if len(text) > limit {
		return text[:limit-1] + "…"
	}
	return text
}

// safeText drops terminal control characters from model and account data
// before styling. Newlines survive for assistant prose; escape sequences do
// not get a chance to become terminal commands.
func safeText(text string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, text)
}

func (m *tuiModel) historyUp() {
	if len(m.history) == 0 {
		return
	}
	if m.historyIndex == len(m.history) {
		m.historyDraft = m.input.Value()
	}
	if m.historyIndex > 0 {
		m.historyIndex--
		m.input.SetValue(m.history[m.historyIndex])
	}
}

func (m *tuiModel) historyDown() {
	if m.historyIndex >= len(m.history) {
		return
	}
	m.historyIndex++
	if m.historyIndex == len(m.history) {
		m.input.SetValue(m.historyDraft)
		return
	}
	m.input.SetValue(m.history[m.historyIndex])
}

func (m *tuiModel) View() tea.View {
	if m.quitting {
		return tea.NewView("")
	}
	width := m.contentWidth()
	var sections []string
	if m.welcome {
		sections = append(sections, m.renderWelcome(width))
	}
	if m.currentUser != "" {
		sections = append(sections, m.renderTurn())
	}
	switch m.phase {
	case phaseQuestion:
		sections = append(sections, m.renderQuestion(width))
	case phaseConsent:
		sections = append(sections, m.renderConsent(width))
	default:
		sections = append(sections, m.renderComposer(width))
	}
	content := "  " + strings.ReplaceAll(strings.Join(sections, "\n\n"), "\n", "\n  ")
	view := tea.NewView(content)
	view.WindowTitle = "ihme agent"
	return view
}

func (m *tuiModel) contentWidth() int {
	width := m.width - 4
	if width <= 0 {
		width = 76
	}
	return min(width, 92)
}

func (m *tuiModel) renderWelcome(width int) string {
	mode := "changes ask first"
	if m.grant == GrantAuto {
		mode = "changes allowed this session"
	}
	title := m.styles.strong.Render("ihme") + " " + m.styles.accent.Render("agent")
	meta := m.styles.muted.Render(safeText(m.appleID) + " · " + mode)
	help := m.styles.muted.Render("Try  “new address for github”   “find netflix”   “tag linear as work”")
	return lipgloss.NewStyle().Width(width).Render(title + "  " + meta + "\n\n" + help)
}

func (m *tuiModel) renderTurn() string {
	width := m.contentWidth()
	var lines []string
	if m.currentUser != "" {
		lines = append(lines, m.styles.accent.Render("›")+" "+m.styles.strong.Render(safeText(m.currentUser)))
	}
	for _, step := range m.steps {
		symbol, style := "✓", m.styles.success
		switch step.level {
		case stepInfo:
			symbol, style = "?", m.styles.accent
		case stepWarn:
			symbol, style = "–", m.styles.warning
		case stepError:
			symbol, style = "!", m.styles.danger
		}
		lines = append(lines, "  "+style.Render(symbol)+" "+safeText(step.text))
		if step.detail != "" {
			lines = append(lines, "    "+m.styles.muted.Render(safeText(step.detail)))
		}
	}
	if strings.TrimSpace(m.assistant) != "" {
		lines = append(lines, "  "+renderInline(strings.TrimSpace(m.assistant), m.styles))
	}
	if m.phase == phaseRunning && m.activity != "" {
		lines = append(lines, "  "+m.spinner.View()+" "+m.styles.muted.Render(m.activity))
	}
	return lipgloss.NewStyle().Width(width).Render(strings.Join(lines, "\n"))
}

func (m *tuiModel) renderComposer(width int) string {
	if m.phase == phaseRunning {
		m.input.Placeholder = "Write your next message…"
	} else {
		m.input.Placeholder = "Ask about your Hide My Email addresses"
	}
	m.input.Prompt = "› "
	m.input.SetWidth(max(12, width-2))
	help := "enter send · ↑ history · ctrl-d exit"
	if m.phase == phaseRunning {
		help = "working · type your next message · esc stop"
	}
	return m.input.View() + "\n" + m.styles.muted.Render(help)
}

func (m *tuiModel) renderQuestion(width int) string {
	if m.prompt == nil {
		return ""
	}
	m.input.Prompt = "› "
	m.input.Placeholder = "Your answer"
	m.input.SetWidth(max(12, width-2))
	title := m.styles.accent.Render("?") + " " + m.styles.strong.Render(safeText(m.prompt.prompt.Title))
	return lipgloss.NewStyle().Width(width).Render(title + "\n" + m.input.View() + "\n" + m.styles.muted.Render("enter answer · esc skip"))
}

func (m *tuiModel) renderConsent(width int) string {
	if m.prompt == nil {
		return ""
	}
	title := m.styles.warning.Render("!") + " " + m.styles.strong.Render(safeText(m.prompt.prompt.Title))
	detail := m.styles.muted.Render(safeText(m.prompt.prompt.Detail))
	labels := []string{"Allow once", "Deny", "Always this run"}
	buttons := make([]string, len(labels))
	for i, label := range labels {
		if i == m.choice {
			buttons[i] = m.styles.selected.Render(label)
		} else {
			buttons[i] = m.styles.button.Render(label)
		}
	}
	help := m.styles.muted.Render("←/→ choose · enter confirm · y/n/a shortcut")
	return lipgloss.NewStyle().Width(width).Render(title + "\n" + detail + "\n\n" + strings.Join(buttons, "  ") + "\n" + help)
}

// renderInline styles the markdown the model is invited to use:
// **bold**, *italic*, `code`. Underscores stay literal — they are
// address characters here, not emphasis.
func renderInline(text string, styles tuiStyles) string {
	runes := []rune(safeText(text))
	var out, seg strings.Builder
	var bold, italic, code bool
	emit := func() {
		if seg.Len() == 0 {
			return
		}
		style := lipgloss.NewStyle()
		switch {
		case code:
			style = styles.accent
		default:
			if bold {
				style = style.Bold(true)
			}
			if italic {
				style = style.Italic(true)
			}
		}
		out.WriteString(style.Render(seg.String()))
		seg.Reset()
	}
	for i := 0; i < len(runes); {
		r := runes[i]
		switch {
		case r == '`':
			emit()
			code = !code
			i++
		case r == '*' && !code:
			emit()
			if i+1 < len(runes) && runes[i+1] == '*' {
				bold = !bold
				i += 2
			} else {
				italic = !italic
				i++
			}
		default:
			seg.WriteRune(r)
			i++
		}
	}
	emit()
	return out.String()
}

func runTUI(ctx context.Context, svc *app.Service, appleID string, grant GrantMode, effort string) error {
	bridge := &tuiBridge{}
	s, err := newSession(svc, appleID, "", grant, effort, sessionIO{
		ask:    bridge.ask,
		events: bridge.emit,
	})
	if err != nil {
		return err
	}
	model := newTUIModel(ctx, s, appleID, grant)
	program := tea.NewProgram(model,
		tea.WithContext(ctx),
		tea.WithInput(os.Stdin),
		tea.WithOutput(os.Stderr),
	)
	bridge.program = program
	_, err = program.Run()
	model.stopTurn()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return err
}
