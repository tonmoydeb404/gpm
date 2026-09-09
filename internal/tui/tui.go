// Package tui provides the interactive terminal interface for gpm:
// a keyboard-first, screen-stack navigator over the same internal
// packages the CLI uses, so both frontends mutate state through
// identical, tested code paths.
//
// Navigation contract for every screen:
//
//	up/down (or k/j)  move the cursor
//	enter             select
//	esc               go back one screen
//	q                 quit (menus and passive screens)
//	ctrl+c            quit
package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/tonmoydeb/gpm/internal/config"
	"github.com/tonmoydeb/gpm/internal/doctor"
	"github.com/tonmoydeb/gpm/internal/importer"
)

// saveIntent tells the savedMsg handler where to go once a mutation
// has been persisted and synced.
type saveIntent int

const (
	intentStay    saveIntent = iota // status only
	intentPop                       // leave the (detail) screen behind the form
	intentSuccess                   // push the success screen for m.outcome
)

// createOutcome is everything the success screen needs to explain
// what just happened and what to do next.
type createOutcome struct {
	Title    string   // "SSH profile created"
	Lines    []string // summary lines ("Profile: company")
	KeyBlock string   // generated public key, if any
	NextStep string   // plain-language follow-up
	SSHName  string   // ssh profile for the copy/test actions
}

// Model is the root bubbletea model. The interface is a stack of
// screens; forms and menus own input while on top of the stack.
type Model struct {
	cfg   *config.Config
	stack []*screen

	spinner spinner.Model
	busy    string

	status string
	isErr  bool
	// statusMsg is the message savedMsg should show after a specific
	// mutation ("profile deleted"); empty means a generic "saved".
	statusMsg string

	intent  saveIntent
	outcome *createOutcome
	report  *importer.Report // result of the last scan

	width, height int
}

// NewModel assembles the TUI model for a loaded config.
func NewModel(cfg *config.Config) *Model {
	home := homeScreen()
	return &Model{
		cfg:     cfg,
		stack:   []*screen{&home},
		spinner: spinner.New(spinner.WithSpinner(spinner.Dot), spinner.WithStyle(busyStyle)),
	}
}

// Run launches the interface on the alternate screen.
func Run(cfg *config.Config) error {
	if _, err := tea.NewProgram(NewModel(cfg), tea.WithAltScreen()).Run(); err != nil {
		return fmt.Errorf("tui: %w", err)
	}
	return nil
}

// Init implements tea.Model.
func (m *Model) Init() tea.Cmd { return nil }

// push adds a screen on top of the stack.
func (m *Model) push(s screen) tea.Cmd {
	if s.kind == kindText && s.scroll {
		s.viewport.Width = m.width
		s.viewport.Height = m.bodyHeight()
	}
	m.stack = append(m.stack, &s)
	if s.kind == kindForm {
		return s.form.Init()
	}
	return nil
}

// pop returns to the previous screen; the home screen is sticky, so
// popping it quits instead.
func (m *Model) pop() tea.Cmd {
	if len(m.stack) <= 1 {
		return tea.Quit
	}
	m.stack = m.stack[:len(m.stack)-1]
	return nil
}

// replaceTop swaps the top screen for s, preserving the cursor when
// the item count allows it.
func (m *Model) replaceTop(s screen) {
	old := m.top()
	if old.cursor < len(s.items) {
		s.cursor = old.cursor
	}
	m.stack[len(m.stack)-1] = &s
}

// rebuildTop refreshes the top screen from the current config.
func (m *Model) rebuildTop() {
	s := m.top()
	if s.rebuild != nil {
		m.replaceTop(s.rebuild(m))
	}
}

func (m *Model) top() *screen { return m.stack[len(m.stack)-1] }

// bodyHeight returns how many rows the body area may use.
func (m *Model) bodyHeight() int {
	h := m.height - 6 // title, spacing, status, hint
	if h < 3 {
		h = 3
	}
	return h
}

// Update implements tea.Model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		for _, s := range m.stack {
			if s.kind == kindText && s.scroll {
				s.viewport.Width = m.width
				s.viewport.Height = m.bodyHeight()
			}
		}
		return m, nil

	case spinner.TickMsg:
		if m.busy != "" {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil

	case savedMsg:
		m.busy = ""
		if msg.err != nil {
			m.setStatus(false, "save failed: "+msg.err.Error())
			m.intent = intentStay
			return m, nil
		}
		intent := m.intent
		m.intent = intentStay
		outcome := m.outcome
		switch intent {
		case intentSuccess:
			m.setStatus(true, m.statusMsg)
			return m, m.pushSuccess(outcome)
		case intentPop:
			m.setStatus(true, m.statusMsg)
			cmd := m.pop()
			m.rebuildTop()
			return m, cmd
		default:
			m.setStatus(true, orStatus(m.statusMsg, "saved & synced"))
			m.rebuildTop()
			return m, nil
		}

	case authTestedMsg:
		m.busy = ""
		if msg.err != nil {
			m.setStatus(false, "connection test failed: "+msg.err.Error())
		} else if msg.username != "" {
			m.setStatus(true, "authenticated as "+msg.username)
		} else {
			m.setStatus(true, "authenticated to "+msg.host)
		}
		return m, nil

	case doctorRanMsg:
		m.busy = ""
		s := doctorScreen(msg.results)
		m.push(s)
		m.setStatus(true, "doctor finished")
		return m, nil

	case scanRanMsg:
		m.busy = ""
		if msg.err != nil {
			m.setStatus(false, "scan failed: "+msg.err.Error())
			return m, nil
		}
		m.report = msg.report
		m.push(m.scanScreen(msg.report))
		m.setStatus(true, "scan finished")
		return m, nil

	case tea.KeyMsg:
		if m.busy != "" {
			return m, nil // input is ignored while a task runs
		}
	}

	if m.busy != "" {
		return m, nil
	}

	// The top screen owns all remaining input.
	s := m.top()
	switch s.kind {
	case kindForm:
		return m.updateForm(msg)
	case kindText:
		return m.updateText(msg)
	default:
		return m.updateMenu(msg)
	}
}

// updateMenu routes input on menu screens.
func (m *Model) updateMenu(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, ok := msg.(tea.KeyMsg)
	if !ok {
		return m, nil
	}
	s := m.top()
	if s.menuCursor(key.String()) {
		return m, nil
	}
	switch key.String() {
	case "enter":
		return m, m.runAction(s.items[s.cursor].action)
	case "esc":
		return m, m.pop()
	case "q", "ctrl+c":
		return m, tea.Quit
	}
	return m, nil
}

// updateText routes input on passive screens: esc goes back, q quits,
// and the viewport scrolls long content.
func (m *Model) updateText(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch key := msg.(type) {
	case tea.KeyMsg:
		switch key.String() {
		case "esc":
			return m, m.pop()
		case "q", "ctrl+c":
			return m, tea.Quit
		case "c":
			if name := m.top().copyName; name != "" {
				m.copyPublicKey(name)
			}
			return m, nil
		}
	}
	if s := m.top(); s.scroll {
		var cmd tea.Cmd
		s.viewport, cmd = s.viewport.Update(msg)
		return m, cmd
	}
	return m, nil
}

// View implements tea.Model.
func (m *Model) View() string {
	if m.busy != "" {
		return m.centered(m.spinner.View() + " " + busyStyle.Render(m.busy))
	}
	if m.width == 0 {
		return "loading…"
	}
	if m.width < 40 || m.height < 14 {
		return m.centered(hintTextStyle.Render("terminal too small — gpm needs at least 40×14"))
	}

	s := m.top()
	var body string
	switch s.kind {
	case kindForm:
		if status := m.statusLine(); status != "" {
			body = status + "\n\n" + s.form.View()
		} else {
			body = s.form.View()
		}
		return body + "\n\n" + hintStyle.Render("enter confirm · esc back")
	case kindText:
		body = titleStyle.Render(s.title) + "\n\n" + s.renderText()
	case kindMenu:
		body = s.renderMenu()
	}
	if status := m.statusLine(); status != "" {
		body += "\n\n" + status
	}
	return body + "\n\n" + hintStyle.Render(s.hintLine())
}

// hintLine picks the bottom hint for the screen kind.
func (s *screen) hintLine() string {
	switch {
	case s.kind == kindText && s.copyName != "":
		return "c copy · esc back · q quit"
	case s.kind == kindText:
		return "esc back · q quit"
	case s.title == "GPM":
		return "↑/↓ move · enter select · q quit"
	default:
		return "↑/↓ move · enter select · esc back · q quit"
	}
}

func (m *Model) setStatus(ok bool, msg string) {
	m.status = msg
	m.isErr = !ok
}

func (m *Model) statusLine() string {
	switch {
	case m.status == "":
		return ""
	case m.isErr:
		return statusErrStyle.Render("✗ " + m.status)
	default:
		return statusOKStyle.Render("✓ " + m.status)
	}
}

func orStatus(msg, fallback string) string {
	if msg != "" {
		return msg
	}
	return fallback
}

// pushSuccess builds and pushes the outcome screen after a creation.
func (m *Model) pushSuccess(o *createOutcome) tea.Cmd {
	if o == nil {
		return nil
	}
	var b strings.Builder
	b.WriteString(passStyle.Render("✓ "+o.Title) + "\n\n")
	for _, l := range o.Lines {
		b.WriteString(l + "\n")
	}
	if o.KeyBlock != "" {
		b.WriteString("\nYour public key is ready:\n\n" + keyBlockStyle.Render(o.KeyBlock) + "\n")
	}
	if o.NextStep != "" {
		b.WriteString("\nNext step:\n" + o.NextStep + "\n")
	}

	var items []menuItem
	if o.SSHName != "" {
		items = append(items,
			menuItem{label: "Copy Public Key", desc: "put the key on your clipboard", action: "success:copy"},
			menuItem{label: "Test Connection", desc: "try authenticating now", action: "success:test"},
		)
	}
	items = append(items, menuItem{label: "Done", desc: "back to the main menu", action: "success:done"})

	s := screen{kind: kindMenu, title: "Success", body: strings.TrimRight(b.String(), "\n"), items: items}
	return m.push(s)
}

// doctorScreen renders doctor results as a scrollable text screen.
func doctorScreen(results []doctor.Result) screen {
	s := screen{
		kind:    kindText,
		title:   "Doctor",
		content: renderDoctorResults(results),
		scroll:  true,
	}
	s.viewport = viewport.New(0, 0)
	s.viewport.SetContent(s.content)
	return s
}
