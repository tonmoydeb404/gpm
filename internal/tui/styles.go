package tui

import "github.com/charmbracelet/lipgloss"

// The palette reuses huh's ThemeCharm colors so the menus and forms
// read as one interface, in both dark and light terminals.
var (
	colAccent  = lipgloss.AdaptiveColor{Light: "#5A56E0", Dark: "#7571F9"} // indigo
	colFuchsia = lipgloss.Color("#F780E2")
	colGreen   = lipgloss.AdaptiveColor{Light: "#02BA84", Dark: "#02BF87"}
	colRed     = lipgloss.AdaptiveColor{Light: "#FF4672", Dark: "#ED567A"}
	colYellow  = lipgloss.AdaptiveColor{Light: "#B7791F", Dark: "#E5C07B"}
	colText    = lipgloss.AdaptiveColor{Light: "#1a1a1a", Dark: "#dddddd"}
	colDim     = lipgloss.AdaptiveColor{Light: "#909090", Dark: "#737373"}
)

// status and chrome.
var (
	statusOKStyle = lipgloss.NewStyle().
			Foreground(colGreen)

	statusErrStyle = lipgloss.NewStyle().
			Foreground(colRed)

	busyStyle = lipgloss.NewStyle().
			Foreground(colFuchsia)

	hintStyle = lipgloss.NewStyle().
			Foreground(colDim)

	hintTextStyle = lipgloss.NewStyle().
			Foreground(colDim).Italic(true)

	sectionTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colAccent)

	tableHeaderStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colDim)

	passStyle   = lipgloss.NewStyle().Foreground(colGreen)
	warnStyle   = lipgloss.NewStyle().Foreground(colYellow)
	failStyle   = lipgloss.NewStyle().Foreground(colRed)
	detailStyle = lipgloss.NewStyle().Foreground(colText)
)

// menu and screen chrome.
var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(colAccent)
	cursorStyle   = lipgloss.NewStyle().Bold(true).Foreground(colAccent)
	descStyle     = lipgloss.NewStyle().Foreground(colDim).Italic(true)
	keyBlockStyle = lipgloss.NewStyle().Foreground(colText)
)
