package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

// screenKind identifies the three screen flavors.
type screenKind int

const (
	kindMenu screenKind = iota // selectable list; most of the interface
	kindForm                   // a huh form
	kindText                   // passive content (public key, doctor)
)

// menuItem is one selectable row of a menu screen.
type menuItem struct {
	label  string
	desc   string // shown under the highlighted row
	action string // routed by runAction on Enter
}

// screen is one entry of the navigation stack.
type screen struct {
	kind  screenKind
	title string

	// menu screens
	items  []menuItem
	cursor int
	body   string // optional block rendered above the items (success screens)

	// form screens
	form      *huh.Form
	formKind  formKind
	formState *formState

	// text screens
	content  string
	viewport viewport.Model
	scroll   bool
	copyName string // ssh profile name for the "c copy" shortcut

	// rebuild re-derives this screen from the current config after a
	// mutation. nil for screens that cannot go stale (forms, success).
	rebuild func(m *Model) screen
}

// menuCursor moves the selection cursor of a menu screen. It reports
// whether the key was consumed.
func (s *screen) menuCursor(key string) bool {
	switch key {
	case "up", "k":
		if s.cursor > 0 {
			s.cursor--
		}
		return true
	case "down", "j":
		if s.cursor < len(s.items)-1 {
			s.cursor++
		}
		return true
	}
	return false
}

// renderMenu renders the menu: title, optional body block, and the
// items with a cursor marker plus the highlighted row's description.
func (s *screen) renderMenu() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(s.title) + "\n\n")
	if s.body != "" {
		b.WriteString(s.body + "\n\n")
	}
	for i, it := range s.items {
		if i == s.cursor {
			b.WriteString(cursorStyle.Render("> "+it.label) + "\n")
			if it.desc != "" {
				b.WriteString("  " + descStyle.Render(it.desc) + "\n")
			}
		} else {
			b.WriteString("  " + it.label + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// renderText renders a passive text screen, scrolling when needed.
func (s *screen) renderText() string {
	if s.scroll {
		return s.viewport.View()
	}
	return s.content
}

// centered places s in the middle of the terminal.
func (m *Model) centered(s string) string {
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, s)
}
