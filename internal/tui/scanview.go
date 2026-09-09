package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/tonmoydeb/gpm/internal/dirs"
	"github.com/tonmoydeb/gpm/internal/importer"
)

// scanScreen turns a scan report into a menu with follow-up actions.
func (m *Model) scanScreen(rep *importer.Report) screen {
	fresh := rep.Fresh(m.cfg)
	s := screen{
		kind:  kindMenu,
		title: "Scan & Import",
		body:  renderScanSummary(rep, fresh),
		items: []menuItem{
			{label: "Import Selected Accounts", desc: fmt.Sprintf("%d account(s) available", len(fresh)), action: "scan:import"},
			{label: "Re-scan", desc: "run the scan again", action: "home:scan"},
			{label: "Back", action: "back"},
		},
	}
	if len(fresh) == 0 {
		s.items[0] = menuItem{label: "Import Selected Accounts", desc: "nothing new to import", action: "scan:none"}
	}
	return s
}

// renderScanSummary renders the concise report shown above the menu.
func renderScanSummary(rep *importer.Report, fresh []importer.Candidate) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Config scan · %s\n\n", rep.Duration.Round(10*time.Millisecond))

	if len(fresh) == 0 {
		b.WriteString("No new accounts detected.\n")
	} else {
		b.WriteString("Detected accounts:\n")
		for _, c := range fresh {
			email := c.Email
			if email == "" {
				email = "(email unknown)"
			}
			alias := c.HostAlias
			if alias == "" {
				alias = "global"
			}
			line := fmt.Sprintf("• %s [%s] — %s — %s", c.Username, alias, strings.Join(c.Hosts, ", "), email)
			if len(c.Directories) > 0 {
				shortened := make([]string, 0, len(c.Directories))
				for _, d := range c.Directories {
					shortened = append(shortened, dirs.Shorten(d))
				}
				line += " — " + strings.Join(shortened, ", ")
			}
			b.WriteString(line + "\n")
		}
	}

	if len(rep.Warnings) > 0 {
		fmt.Fprintf(&b, "\n%d note(s) — run `gpm scan` for details.\n", len(rep.Warnings))
	}
	return strings.TrimRight(b.String(), "\n")
}
