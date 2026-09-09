package tui

import (
	"fmt"
	"strings"

	"github.com/tonmoydeb404/gpm/internal/doctor"
)

// renderDoctorResults formats doctor check outcomes: a pass/warn/fail
// summary followed by one row per check.
func renderDoctorResults(results []doctor.Result) string {
	if len(results) == 0 {
		return hintTextStyle.Render("no results")
	}

	var pass, warn, fail int
	for _, r := range results {
		switch r.Status {
		case doctor.Pass:
			pass++
		case doctor.Warn:
			warn++
		case doctor.Fail:
			fail++
		}
	}
	summary := fmt.Sprintf("%s   %s   %s",
		passStyle.Render(fmt.Sprintf("✓ %d pass", pass)),
		warnStyle.Render(fmt.Sprintf("! %d warn", warn)),
		failStyle.Render(fmt.Sprintf("✗ %d fail", fail)),
	)

	var b strings.Builder
	b.WriteString(summary + "\n")
	for _, r := range results {
		var icon string
		switch r.Status {
		case doctor.Pass:
			icon = passStyle.Render("✓")
		case doctor.Warn:
			icon = warnStyle.Render("!")
		case doctor.Fail:
			icon = failStyle.Render("✗")
		}
		b.WriteString("\n " + icon + " " + detailStyle.Render(r.Name) + "\n")
		if r.Detail != "" {
			b.WriteString("     " + hintTextStyle.Render(r.Detail) + "\n")
		}
		if r.Hint != "" {
			b.WriteString("     " + hintTextStyle.Render(r.Hint) + "\n")
		}
	}
	return b.String()
}
