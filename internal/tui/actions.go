package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/atotto/clipboard"

	"github.com/tonmoydeb/gpm/internal/auth"
	"github.com/tonmoydeb/gpm/internal/config"
	"github.com/tonmoydeb/gpm/internal/doctor"
	"github.com/tonmoydeb/gpm/internal/sshkey"
	"github.com/tonmoydeb/gpm/internal/sync"
)

// savedMsg reports the outcome of persisting the config and
// regenerating the managed ssh/gitconfig artifacts.
type savedMsg struct{ err error }

// authTestedMsg reports the outcome of a live SSH auth test.
type authTestedMsg struct {
	name     string
	host     string
	username string
	err      error
}

// doctorRanMsg carries the results of a doctor run.
type doctorRanMsg struct {
	results []doctor.Result
	network bool
}

// saveCmd persists the config and regenerates managed artifacts.
func saveCmd(cfg *config.Config) tea.Cmd {
	return func() tea.Msg {
		return savedMsg{err: sync.All(cfg)}
	}
}

// testAuthCmd runs a live SSH auth test against a provider host.
func testAuthCmd(name, keyPath, host string) tea.Cmd {
	return func() tea.Msg {
		res, err := auth.Test("git@"+host, keyPath)
		if err != nil {
			return authTestedMsg{name: name, host: host, err: err}
		}
		return authTestedMsg{name: name, host: host, username: res.Username}
	}
}

// runDoctorCmd executes the doctor checks, optionally over the network.
func runDoctorCmd(cfg *config.Config, network bool) tea.Cmd {
	return func() tea.Msg {
		return doctorRanMsg{results: doctor.Run(cfg, doctor.Options{Network: network}), network: network}
	}
}

// scheduleSave marks the pending post-save intent, shows the busy
// spinner, and starts the save+sync pipeline.
func (m *Model) scheduleSave(intent saveIntent, statusMsg string) tea.Cmd {
	m.intent = intent
	m.statusMsg = statusMsg
	m.busy = "saving & syncing…"
	return tea.Batch(saveCmd(m.cfg), m.spinner.Tick)
}

// copyPublicKey puts an ssh profile's public key on the clipboard.
func (m *Model) copyPublicKey(name string) {
	p, ok := m.cfg.SSHProfiles[name]
	if !ok || p.KeyPath == "" || !sshkey.Exists(p.KeyPath) {
		m.setStatus(false, "no key on disk; generate one first")
		return
	}
	pub, err := sshkey.PublicKey(p.KeyPath)
	if err != nil {
		m.setStatus(false, err.Error())
		return
	}
	if err := clipboard.WriteAll(strings.TrimSpace(pub)); err != nil {
		m.setStatus(false, "clipboard unavailable: "+err.Error())
		return
	}
	m.setStatus(true, "public key copied to clipboard")
}

// testConnectionFor runs a live auth test against the profile's first
// provider. The result arrives as authTestedMsg.
func (m *Model) testConnectionFor(name string) tea.Cmd {
	p, ok := m.cfg.SSHProfiles[name]
	if !ok {
		m.setStatus(false, "ssh profile "+name+" does not exist")
		return nil
	}
	if len(p.Providers) == 0 {
		m.setStatus(false, "profile has no providers")
		return nil
	}
	if p.KeyPath == "" || !sshkey.Exists(p.KeyPath) {
		m.setStatus(false, "no key on disk; generate one first")
		return nil
	}
	m.busy = "testing connection to " + p.Providers[0] + "…"
	return tea.Batch(testAuthCmd(name, p.KeyPath, p.Providers[0]), m.spinner.Tick)
}

// runDoctor triggers a doctor run as an async command.
func (m *Model) runDoctor(network bool) tea.Cmd {
	m.busy = "running doctor (this may take a moment)…"
	return tea.Batch(runDoctorCmd(m.cfg, network), m.spinner.Tick)
}
