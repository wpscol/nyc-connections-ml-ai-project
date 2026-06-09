package main

import (
	"fmt"
	"io"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ── events ─────────────────────────────────────────────────────────────────

type tuiEvent interface{ isTuiEvent() }

type (
	tuiLogLine    struct{ text string } // append to left pane
	tuiThinkChunk struct{ text string } // streaming token for right pane
	tuiThinkDone  struct{}              // model called a tool; dim the right pane
	tuiStatus     struct{ text string } // update status bar
	tuiDone       struct{ err error }   // solver finished; quit
)

func (tuiLogLine) isTuiEvent()    {}
func (tuiThinkChunk) isTuiEvent() {}
func (tuiThinkDone) isTuiEvent()  {}
func (tuiStatus) isTuiEvent()     {}
func (tuiDone) isTuiEvent()       {}

// ── sender ──────────────────────────────────────────────────────────────────

// tuiSender is passed to the solver goroutine and implements io.Writer so that
// log.SetOutput(sender) redirects all log output to the TUI left pane.
type tuiSender struct{ ch chan<- tuiEvent }

func (s *tuiSender) Log(text string)    { s.ch <- tuiLogLine{text} }
func (s *tuiSender) Think(text string)  { s.ch <- tuiThinkChunk{text} }
func (s *tuiSender) ThinkDone()         { s.ch <- tuiThinkDone{} }
func (s *tuiSender) Status(text string) { s.ch <- tuiStatus{text} }
func (s *tuiSender) Done(err error)     { s.ch <- tuiDone{err} }

// Write strips the "YYYY/MM/DD HH:MM:SS " prefix the log package adds (20 chars)
// then forwards the line to the left pane.
func (s *tuiSender) Write(p []byte) (int, error) {
	text := strings.TrimRight(string(p), "\n")
	if len(text) >= 20 {
		text = text[20:]
	}
	if text != "" {
		s.ch <- tuiLogLine{text}
	}
	return len(p), nil
}

var _ io.Writer = (*tuiSender)(nil)

// ── styles ──────────────────────────────────────────────────────────────────

var (
	colorLogBorder   = lipgloss.Color("240")
	colorThinkBorder = lipgloss.Color("62")
	colorFocusBorder = lipgloss.Color("214") // bright orange: the focused (scrollable) pane

	styleThinkActive = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	styleThinkDone   = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	styleScrollTag   = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))

	styleLog = map[string]lipgloss.Style{
		"tool_call":   lipgloss.NewStyle().Foreground(lipgloss.Color("33")),
		"tool_result": lipgloss.NewStyle().Foreground(lipgloss.Color("244")),
		"correct":     lipgloss.NewStyle().Foreground(lipgloss.Color("40")),
		"wrong":       lipgloss.NewStyle().Foreground(lipgloss.Color("196")),
		"oneaway":     lipgloss.NewStyle().Foreground(lipgloss.Color("214")),
		"stat":        lipgloss.NewStyle().Foreground(lipgloss.Color("220")),
		"stage":       lipgloss.NewStyle().Foreground(lipgloss.Color("141")),
		"error":       lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196")),
		"info":        lipgloss.NewStyle().Foreground(lipgloss.Color("250")),
	}

	styleHeader = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("245"))

	styleStatus = lipgloss.NewStyle().
			Background(lipgloss.Color("235")).
			Foreground(lipgloss.Color("246")).
			Padding(0, 1)
)

// ── model ───────────────────────────────────────────────────────────────────

type tuiModel struct {
	w, h       int
	logLines   []string // pre-coloured with lipgloss
	thinkBuf   string   // accumulated reasoning text (plain string — model is copied by value)
	thinkDone  bool
	statusText string
	events     <-chan tuiEvent

	// scrolling: offset = lines from the bottom; 0 = follow live tail.
	focus       int // 0 = log pane, 1 = think pane
	logScroll   int
	thinkScroll int
}

func newTUI(events <-chan tuiEvent, status string) tuiModel {
	return tuiModel{events: events, statusText: status}
}

// layout returns the inner text dimensions of each pane and the pane height.
// Kept in one place so Update (scroll clamping) and View agree exactly.
func (m tuiModel) layout() (leftIW, rightIW, innerH, paneH int) {
	leftW := max(24, m.w*2/5)
	rightW := m.w - leftW
	paneH = m.h - 1          // 1 row for the status bar
	leftIW = max(1, leftW-4) // border(2) + padding(2)
	rightIW = max(1, rightW-4)
	innerH = max(1, paneH-3) // border(2) + header line(1)
	return
}

// curScroll returns a pointer to the focused pane's scroll offset.
func (m *tuiModel) curScroll() *int {
	if m.focus == 1 {
		return &m.thinkScroll
	}
	return &m.logScroll
}

// totalLines is the number of rendered lines in the focused pane's content.
func (m tuiModel) totalLines() int {
	if m.focus == 1 {
		_, rIW, _, _ := m.layout()
		return len(wrapLines(m.thinkBuf, rIW))
	}
	return len(m.logDisplayLines())
}

// logDisplayLines wraps each raw log line to the pane width and colours every
// resulting row, so long tool calls / IDs are shown in full instead of cut off.
func (m tuiModel) logDisplayLines() []string {
	lIW, _, _, _ := m.layout()
	var out []string
	for _, raw := range m.logLines {
		sty := logStyle(raw)
		wrapped := wrapLines(raw, lIW)
		if len(wrapped) == 0 {
			out = append(out, "")
			continue
		}
		for _, wl := range wrapped {
			out = append(out, sty.Render(wl))
		}
	}
	return out
}

func (m *tuiModel) scrollBy(delta int) {
	_, _, iH, _ := m.layout()
	off := m.curScroll()
	*off += delta
	maxOff := max(0, m.totalLines()-iH)
	if *off > maxOff {
		*off = maxOff
	}
	if *off < 0 {
		*off = 0
	}
}

func (m tuiModel) Init() tea.Cmd { return waitEv(m.events) }

func waitEv(ch <-chan tuiEvent) tea.Cmd {
	return func() tea.Msg { return <-ch }
}

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch ev := msg.(type) {

	case tea.WindowSizeMsg:
		m.w, m.h = ev.Width, ev.Height

	case tea.KeyMsg:
		_, _, iH, _ := m.layout()
		switch ev.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "tab":
			m.focus = 1 - m.focus
		case "up", "k":
			m.scrollBy(1)
		case "down", "j":
			m.scrollBy(-1)
		case "pgup":
			m.scrollBy(iH - 1)
		case "pgdown", "pgdn":
			m.scrollBy(-(iH - 1))
		case "home", "g":
			*m.curScroll() = max(0, m.totalLines()-iH) // scroll to top
		case "end", "G":
			*m.curScroll() = 0 // back to live tail
		}
		return m, nil

	case tuiLogLine:
		m.logLines = append(m.logLines, ev.text) // store raw; coloured + wrapped at render
		// keep the viewport anchored if the user has scrolled up — a raw line may
		// wrap to several display rows, so advance by that many.
		if m.logScroll > 0 {
			lIW, _, _, _ := m.layout()
			k := len(wrapLines(ev.text, lIW))
			if k < 1 {
				k = 1
			}
			m.logScroll += k
		}
		return m, waitEv(m.events)

	case tuiThinkChunk:
		if m.thinkDone { // new generation cycle — clear previous thought
			m.thinkBuf = ""
			m.thinkDone = false
			m.thinkScroll = 0
		}
		m.thinkBuf += ev.text
		return m, waitEv(m.events)

	case tuiThinkDone:
		m.thinkDone = true
		return m, waitEv(m.events)

	case tuiStatus:
		m.statusText = ev.text
		return m, waitEv(m.events)

	case tuiDone:
		if ev.err != nil {
			m.logLines = append(m.logLines, "fatal: "+ev.err.Error())
		}
		return m, tea.Quit
	}

	return m, nil
}

func (m tuiModel) View() string {
	if m.w == 0 {
		return "initialising…"
	}

	lIW, rIW, iH, paneH := m.layout()

	// ── left: game log ──────────────────────────────────────────────────────
	logLines := m.logDisplayLines()
	logWin, logOff := windowLines(logLines, iH, m.logScroll)
	logHeader := styleHeader.Render("Game Log") + scrollTag(logOff, len(logLines), iH)
	logContent := logHeader + "\n" + strings.Join(logWin, "\n")
	leftBorder := colorLogBorder
	if m.focus == 0 {
		leftBorder = colorFocusBorder
	}
	leftPane := lipgloss.NewStyle().
		Width(lIW).Height(paneH-2).
		MaxWidth(lIW+4).MaxHeight(paneH). // hard clip: never exceed pane bounds
		Border(lipgloss.RoundedBorder()).
		BorderForeground(leftBorder).
		Padding(0, 1).
		Render(logContent)

	// ── right: thinking ─────────────────────────────────────────────────────
	title := "Thinking…"
	sty := styleThinkActive
	if m.thinkDone {
		title = "Thinking"
		sty = styleThinkDone
	}
	wrapped := wrapLines(m.thinkBuf, rIW)
	var thinkBody string
	thinkOff := 0
	if len(wrapped) == 0 {
		thinkBody = sty.Faint(true).Render("(waiting for model…)")
	} else {
		win, off := windowLines(wrapped, iH, m.thinkScroll)
		thinkOff = off
		// truncate each line to the pane width so lipgloss never re-wraps it into
		// an extra row — that overflow is what corrupts the screen layout.
		truncR := lipgloss.NewStyle().MaxWidth(rIW)
		styled := make([]string, len(win))
		for i, l := range win {
			styled[i] = sty.Render(truncR.Render(l))
		}
		thinkBody = strings.Join(styled, "\n")
	}
	thinkHeader := styleHeader.Render(title) + scrollTag(thinkOff, len(wrapped), iH)
	thinkContent := thinkHeader + "\n" + thinkBody
	rightBorder := colorThinkBorder
	if m.focus == 1 {
		rightBorder = colorFocusBorder
	}
	rightPane := lipgloss.NewStyle().
		Width(rIW).Height(paneH-2).
		MaxWidth(rIW+4).MaxHeight(paneH). // hard clip: never exceed pane bounds
		Border(lipgloss.RoundedBorder()).
		BorderForeground(rightBorder).
		Padding(0, 1).
		Render(thinkContent)

	// ── status bar ──────────────────────────────────────────────────────────
	help := "tab focus · ↑↓/jk scroll · pgup/pgdn · g/G top/live · q quit"
	bar := styleStatus.Width(m.w - 2).Render(m.statusText + "  · " + help)

	return lipgloss.JoinHorizontal(lipgloss.Top, leftPane, rightPane) + "\n" + bar
}

// windowLines returns the visible slice of lines for height h at scroll offset
// off (lines from the bottom), plus the clamped offset actually used.
func windowLines(lines []string, h, off int) ([]string, int) {
	total := len(lines)
	maxOff := max(0, total-h)
	if off > maxOff {
		off = maxOff
	}
	if off < 0 {
		off = 0
	}
	end := total - off
	start := max(0, end-h)
	return lines[start:end], off
}

// wrapLines word-wraps text to width w and returns the individual lines.
func wrapLines(text string, w int) []string {
	if text == "" {
		return nil
	}
	return strings.Split(wordWrap(text, w), "\n")
}

// scrollTag renders a small header indicator: live when at the bottom,
// otherwise how many lines up the view is scrolled.
func scrollTag(off, total, h int) string {
	if total <= h {
		return ""
	}
	if off == 0 {
		return styleScrollTag.Render("  ▼ live")
	}
	return styleScrollTag.Render(fmt.Sprintf("  ↑%d", off))
}

// wordWrap inserts newlines at word boundaries so no line exceeds maxW columns.
// Words longer than maxW are hard-broken. Width is counted in runes (not bytes)
// so multi-byte characters don't push lines past the pane edge.
func wordWrap(text string, maxW int) string {
	if maxW <= 0 {
		return text
	}
	var sb strings.Builder
	lineLen := 0
	for _, word := range strings.Fields(text) {
		r := []rune(word)
		// hard-break any word that cannot fit on a line by itself
		for len(r) > maxW {
			if lineLen > 0 {
				sb.WriteByte('\n')
				lineLen = 0
			}
			sb.WriteString(string(r[:maxW]))
			sb.WriteByte('\n')
			r = r[maxW:]
		}
		wl := len(r)
		if lineLen > 0 && lineLen+1+wl > maxW {
			sb.WriteByte('\n')
			lineLen = 0
		} else if lineLen > 0 {
			sb.WriteByte(' ')
			lineLen++
		}
		sb.WriteString(string(r))
		lineLen += wl
	}
	return sb.String()
}

// logStyle picks a lipgloss style based on the log line content.
func logStyle(text string) lipgloss.Style {
	cat := "info"
	switch {
	case strings.HasPrefix(text, "tool call:"):
		cat = "tool_call"
	case strings.HasPrefix(text, "tool result:") && strings.Contains(text, " correct"):
		cat = "correct"
	case strings.HasPrefix(text, "tool result:") && strings.Contains(text, "one_away"):
		cat = "oneaway"
	case strings.HasPrefix(text, "tool result:") && strings.Contains(text, " wrong"):
		cat = "wrong"
	case strings.HasPrefix(text, "tool result:"):
		cat = "tool_result"
	case strings.HasPrefix(text, "stage injected"), strings.HasPrefix(text, "context reminder"):
		cat = "stage"
	case strings.HasPrefix(text, "game ") || strings.HasPrefix(text, "session stats") || strings.HasPrefix(text, "final stats"):
		cat = "stat"
	case strings.Contains(text, "error") || strings.Contains(text, "stuck") || strings.Contains(text, "fatal"):
		cat = "error"
	}
	return styleLog[cat]
}
