package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"

	"github.com/tonmoydeb/gpm/internal/config"
	"github.com/tonmoydeb/gpm/internal/dirs"
	"github.com/tonmoydeb/gpm/internal/gitprofile"
	"github.com/tonmoydeb/gpm/internal/sshkey"
	"github.com/tonmoydeb/gpm/internal/sshprofile"
)

// formKind identifies what a form is and how its completion is
// applied.
type formKind int

const (
	fkSSHCreate formKind = iota
	fkSSHUpdate
	fkSSHDelete
	fkProviderAdd
	fkProviderRemove
	fkGitCreate
	fkGitUpdate
	fkGitDelete
	fkBothCreate
	fkDirAdd
	fkDirRemove
)

// formState holds the variables huh fields bind to, plus the context
// (profile being edited, pending removals) needed to apply the form.
type formState struct {
	// inputs
	provider, hostAlias        string
	username, email, directory string

	// confirmations
	confirm   bool
	removeKey bool

	// context
	editName string // ssh profile being edited
	editUser string // git username being edited
	pick     string // provider host or directory pending removal
}

// tuiKeymap is huh's default keymap with esc added as a form abort
// key, so esc consistently backs out of any form.
var tuiKeymap = func() *huh.KeyMap {
	km := huh.NewDefaultKeyMap()
	km.Quit = key.NewBinding(key.WithKeys("ctrl+c", "esc"), key.WithHelp("esc", "cancel"))
	return km
}()

// tuiTheme matches the chrome palette (styles.go) so forms read as
// part of the same interface.
var tuiTheme = huh.ThemeCharm()

// updateForm delegates input to the active huh form and handles its
// completion or abort. When applying the completed form fails, the
// form stays open — rebuilt from the same state, so everything the
// user typed is still there — with the error on the status line.
func (m *Model) updateForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	s := m.top()
	updated, cmd := s.form.Update(msg)
	form, ok := updated.(*huh.Form)
	if !ok {
		return m, cmd
	}
	s.form = form

	switch form.State {
	case huh.StateCompleted:
		kind, state := s.formKind, s.formState
		stay, cmd := m.applyForm(kind, state)
		if stay {
			rebuilt := m.buildFormScreen(kind, state)
			m.replaceTop(rebuilt)
			return m, rebuilt.form.Init()
		}
		m.pop()
		return m, cmd

	case huh.StateAborted:
		m.pop()
		return m, nil
	}
	return m, cmd
}

// applyForm turns a completed form into mutations plus the follow-up
// save. It reports whether the form must stay open: validation
// failures keep it on screen with the error on the status line,
// cancellations and successes move on.
func (m *Model) applyForm(kind formKind, s *formState) (stay bool, cmd tea.Cmd) {
	switch kind {

	case fkSSHCreate:
		res, err := sshprofile.Add(m.cfg, config.SSHProfile{
			Username:  s.username,
			HostAlias: s.hostAlias,
			Providers: []string{s.provider},
		}, sshprofile.AddOpts{GenerateKey: true})
		if err != nil {
			m.setStatus(false, err.Error())
			return true, nil
		}
		m.outcome = &createOutcome{
			Title:    "SSH profile created",
			Lines:    []string{"Username: " + s.username, "Provider: " + s.provider, aliasLine(s.hostAlias)},
			KeyBlock: strings.TrimSpace(res.PublicKey),
			NextStep: "Add this public key to your provider before using the profile.",
			SSHName:  s.username,
		}
		return false, m.scheduleSave(intentSuccess, "ssh profile created")

	case fkSSHUpdate:
		err := sshprofile.Edit(m.cfg, s.editName, func(p *config.SSHProfile) {
			p.HostAlias = s.hostAlias
		})
		if err != nil {
			m.setStatus(false, err.Error())
			return true, nil
		}
		return false, m.scheduleSave(intentStay, "profile updated")

	case fkSSHDelete:
		if !s.confirm {
			m.setStatus(true, "cancelled")
			return false, nil
		}
		if _, err := sshprofile.Remove(m.cfg, s.editName, sshprofile.RemoveOpts{RemoveKey: s.removeKey}); err != nil {
			m.setStatus(false, err.Error())
			return true, nil
		}
		return false, m.scheduleSave(intentPop, "ssh profile deleted")

	case fkProviderAdd:
		if err := sshprofile.AddProvider(m.cfg, s.editName, s.provider); err != nil {
			m.setStatus(false, err.Error())
			return true, nil
		}
		return false, m.scheduleSave(intentStay, "provider added")

	case fkProviderRemove:
		if !s.confirm {
			m.setStatus(true, "cancelled")
			return false, nil
		}
		if err := sshprofile.RemoveProvider(m.cfg, s.editName, s.pick); err != nil {
			m.setStatus(false, err.Error())
			return true, nil
		}
		return false, m.scheduleSave(intentStay, "provider removed")

	case fkGitCreate:
		res, err := gitprofile.Add(m.cfg, config.GitProfile{
			Username: s.username,
			Email:    s.email,
		}, s.directory)
		if err != nil {
			m.setStatus(false, err.Error())
			return true, nil
		}
		m.outcome = gitOutcome("Git profile created", s.username, s.email, s.directory, res.IsGlobal)
		return false, m.scheduleSave(intentSuccess, "git profile created")

	case fkBothCreate:
		return m.applyBoth(s)

	case fkGitUpdate:
		err := gitprofile.Edit(m.cfg, s.editUser, func(p *config.GitProfile) {
			p.Email = s.email
		})
		if err != nil {
			m.setStatus(false, err.Error())
			return true, nil
		}
		return false, m.scheduleSave(intentStay, "profile updated")

	case fkGitDelete:
		if !s.confirm {
			m.setStatus(true, "cancelled")
			return false, nil
		}
		res, err := gitprofile.Remove(m.cfg, s.editUser)
		if err != nil {
			m.setStatus(false, err.Error())
			return true, nil
		}
		if res.WasGlobal {
			m.statusMsg = "git profile deleted (no global identity is set now)"
		} else {
			m.statusMsg = fmt.Sprintf("git profile deleted (%d mapping(s) removed)", res.RemovedDirs)
		}
		return false, m.scheduleSave(intentPop, m.statusMsg)

	case fkDirAdd:
		if _, err := dirs.Add(m.cfg, s.directory, s.editUser, false); err != nil {
			m.setStatus(false, err.Error())
			return true, nil
		}
		return false, m.scheduleSave(intentStay, "directory mapped")

	case fkDirRemove:
		if !s.confirm {
			m.setStatus(true, "cancelled")
			return false, nil
		}
		if _, err := dirs.Remove(m.cfg, s.pick); err != nil {
			m.setStatus(false, err.Error())
			return true, nil
		}
		return false, m.scheduleSave(intentStay, "directory removed")
	}
	return false, nil
}

// applyBoth runs the combined onboarding: a Git profile and an SSH
// profile generated with the existing defaults. The SSH profile
// username is derived from the GitHub username; no extra questions
// are asked. If the SSH side fails after the Git side succeeded, the
// Git profile is rolled back so retrying starts clean.
func (m *Model) applyBoth(s *formState) (stay bool, cmd tea.Cmd) {
	_, err := gitprofile.Add(m.cfg, config.GitProfile{
		Username: s.username,
		Email:    s.email,
	}, s.directory)
	if err != nil {
		m.setStatus(false, err.Error())
		return true, nil
	}
	name := m.uniqueSSHName(s.username)
	sshRes, err := sshprofile.Add(m.cfg, config.SSHProfile{
		Username:  name,
		HostAlias: s.hostAlias,
		Providers: []string{s.provider},
	}, sshprofile.AddOpts{GenerateKey: true})
	if err != nil {
		delete(m.cfg.GitProfiles, s.username) // roll back the git side
		m.setStatus(false, err.Error())
		return true, nil
	}

	lines := []string{
		"Git identity: " + s.username + " <" + s.email + ">",
		"SSH profile: " + name,
		"SSH provider: " + s.provider,
		aliasLine(s.hostAlias),
	}
	if abs, err := dirs.Normalize(s.directory); err == nil && s.directory != "" {
		lines = append(lines, "Directory: "+dirs.Shorten(abs))
	}
	m.outcome = &createOutcome{
		Title:    "Profile created successfully",
		Lines:    lines,
		KeyBlock: strings.TrimSpace(sshRes.PublicKey),
		NextStep: "Add this public key to your " + s.provider + " account.",
		SSHName:  name,
	}
	return false, m.scheduleSave(intentSuccess, "profile created")
}

// uniqueSSHName derives an unused ssh profile name from base.
func (m *Model) uniqueSSHName(base string) string {
	name := base
	for i := 2; ; i++ {
		if _, exists := m.cfg.SSHProfiles[name]; !exists {
			return name
		}
		name = fmt.Sprintf("%s-%d", base, i)
	}
}

// gitOutcome builds the success screen payload for git-only creates.
func gitOutcome(title, username, email, directory string, isGlobal bool) *createOutcome {
	lines := []string{"Git identity: " + username + " <" + email + ">"}
	var next string
	if isGlobal {
		lines = append(lines, "Scope: global identity")
		next = "This identity applies wherever no directory mapping matches."
	} else {
		if abs, err := dirs.Normalize(directory); err == nil {
			lines = append(lines, "Directory: "+dirs.Shorten(abs))
		}
		next = "Repositories under this directory now commit with this identity."
	}
	return &createOutcome{Title: title, Lines: lines, NextStep: next}
}

// --- validators --------------------------------------------------------

// validSSHUsername rejects unusable or colliding ssh usernames.
func (m *Model) validSSHUsername(skip string) func(string) error {
	return func(username string) error {
		if err := sshprofile.ValidateName(username); err != nil {
			return err
		}
		if username != skip {
			if _, exists := m.cfg.SSHProfiles[username]; exists {
				return fmt.Errorf("ssh profile %q already exists", username)
			}
		}
		return nil
	}
}

// validGitUsername rejects unusable or colliding git usernames.
func (m *Model) validGitUsername(skip string) func(string) error {
	return func(username string) error {
		if err := config.ValidateName(username); err != nil {
			return fmt.Errorf("github username: %w", err)
		}
		if username != skip {
			if _, exists := m.cfg.GitProfiles[username]; exists {
				return fmt.Errorf("git profile %q already exists", username)
			}
		}
		return nil
	}
}

func validEmail(s string) error {
	if !strings.Contains(s, "@") {
		return fmt.Errorf("must be a valid email")
	}
	return nil
}

// validHost validates a provider hostname.
func validHost(s string) error {
	_, err := sshprofile.NormalizeHost(s)
	return err
}

// validHostAlias validates the optional host-alias field: empty makes
// the profile the global ssh identity (only one may be global) and a
// non-empty alias must be usable and unused.
func (m *Model) validHostAlias(skip string) func(string) error {
	return func(alias string) error {
		alias = strings.TrimSpace(alias)
		if err := sshprofile.ValidateAlias(alias); err != nil {
			return fmt.Errorf("host alias: %w", err)
		}
		if alias == "" {
			if global, ok := m.cfg.GlobalSSHProfile(); ok && global.Username != skip {
				return fmt.Errorf("%q is already the global ssh profile — only one profile can be global; enter an alias instead", global.Username)
			}
			return nil
		}
		for username, p := range m.cfg.SSHProfiles {
			if username != skip && p.HostAlias == alias {
				return fmt.Errorf("host alias %q is already used by %q", alias, username)
			}
		}
		return nil
	}
}

// validDirectory validates the optional directory field of create
// forms: empty means global (only allowed while no other profile is
// global), otherwise the directory must be free to map.
func (m *Model) validDirectory() func(string) error {
	return func(dir string) error {
		if dir == "" {
			if global, ok := m.cfg.GlobalGitProfile(); ok {
				return fmt.Errorf("%q is already the global profile — only one profile can be global; enter a directory instead", global.Username)
			}
			return nil
		}
		abs, err := dirs.Normalize(dir)
		if err != nil {
			return err
		}
		if owner, ok := dirs.Owner(m.cfg, abs); ok {
			return fmt.Errorf("%s is already mapped to %q", dirs.Shorten(abs), owner)
		}
		return nil
	}
}

// --- form builders -----------------------------------------------------

const (
	helpUsernameSSH    = "Give this SSH identity a short username so you can recognize it later.\nExamples: company, personal, client"
	helpProvider       = "Enter the Git provider hostname this key will authenticate with.\nExamples: github.com, gitlab.com, git.company.com"
	helpAlias          = "The ssh config alias used to clone with this key.\nExample: gpm-work  →  git clone git@gpm-work:owner/repo.git\nLeave empty to make this the global key — used when no alias\nmatches. Only one profile can be global."
	helpGitUsername    = "Your GitHub username.\nIt will also appear as the author of your Git commits.\nExample: tonmoydeb"
	helpEmail          = "The email Git will use for your commits.\nFor GitHub, use an email associated with your account or your\nGitHub noreply email."
	helpDir            = "The folder whose repositories use this identity, e.g. ~/Works/company.\nLeave empty to make this your global identity — used everywhere no\ndirectory mapping applies. Only one profile can be global."
	helpProviderDomain = "Enter the hostname of your Git provider.\nExamples: github.com, gitlab.com, git.company.com"
)

// aliasLine renders the alias scope for success screens.
func aliasLine(alias string) string {
	if alias == "" {
		return "Host alias: none (global key)"
	}
	return "Host alias: " + alias
}

// buildFormScreen reconstructs a form screen from an existing state,
// preserving everything the user typed. Used when a submitted form
// fails to apply and stays open.
func (m *Model) buildFormScreen(kind formKind, s *formState) screen {
	switch kind {
	case fkSSHCreate:
		return m.sshCreateScreen(s)
	case fkSSHUpdate:
		return m.sshUpdateScreen(s)
	case fkSSHDelete:
		return m.sshDeleteScreen(s)
	case fkProviderAdd:
		return m.providerAddScreen(s)
	case fkProviderRemove:
		return m.providerRemoveScreen(s)
	case fkGitCreate:
		return m.gitCreateScreen(s)
	case fkGitUpdate:
		return m.gitUpdateScreen(s)
	case fkGitDelete:
		return m.gitDeleteScreen(s)
	case fkBothCreate:
		return m.bothCreateScreen(s)
	case fkDirAdd:
		return m.dirAddScreen(s)
	case fkDirRemove:
		return m.dirRemoveScreen(s)
	}
	return screen{kind: kindForm}
}

// formScreen builds the screen for a form.
func formScreen(kind formKind, s *formState, form *huh.Form, title string) screen {
	return screen{
		kind:      kindForm,
		title:     title,
		form:      form.WithKeyMap(tuiKeymap).WithTheme(tuiTheme),
		formKind:  kind,
		formState: s,
	}
}

// openSSHCreate builds the create-SSH-profile form.
func (m *Model) openSSHCreate() tea.Cmd {
	return m.push(m.sshCreateScreen(&formState{provider: "github.com"}))
}

func (m *Model) sshCreateScreen(s *formState) screen {
	return formScreen(fkSSHCreate, s, huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Title("Username").Description(helpUsernameSSH).
				Value(&s.username).Validate(m.validSSHUsername("")),
			huh.NewInput().Title("Provider").Description(helpProvider).
				Value(&s.provider).Validate(validHost),
			huh.NewInput().Title("Host alias (optional)").Description(helpAlias).
				Value(&s.hostAlias).Validate(m.validHostAlias("")),
		),
	), "New SSH profile")
}

// openSSHUpdate edits an SSH profile's host alias; the username is
// fixed.
func (m *Model) openSSHUpdate(username string) tea.Cmd {
	p, ok := m.cfg.SSHProfiles[username]
	if !ok {
		m.setStatus(false, "ssh profile "+username+" does not exist")
		return m.pop()
	}
	return m.push(m.sshUpdateScreen(&formState{editName: username, hostAlias: p.HostAlias}))
}

func (m *Model) sshUpdateScreen(s *formState) screen {
	return formScreen(fkSSHUpdate, s, huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Title("Host alias (optional)").Description(helpAlias).
				Value(&s.hostAlias).Validate(m.validHostAlias(s.editName)),
		).Title("Identity: "+s.editName).Description("The username cannot be changed."),
	), "Update SSH profile")
}

// openSSHDelete confirms deletion of an SSH profile.
func (m *Model) openSSHDelete(name string) tea.Cmd {
	return m.push(m.sshDeleteScreen(&formState{editName: name}))
}

func (m *Model) sshDeleteScreen(s *formState) screen {
	return formScreen(fkSSHDelete, s, huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().Title(fmt.Sprintf("Delete SSH profile %q?", s.editName)).
				Description("Its host stanzas are removed from the managed ssh config.").
				Value(&s.confirm),
			huh.NewConfirm().Title("Also delete the key pair from disk?").
				Value(&s.removeKey),
		),
	), "Delete SSH profile")
}

// openProviderAdd asks for one provider hostname.
func (m *Model) openProviderAdd(name string) tea.Cmd {
	return m.push(m.providerAddScreen(&formState{editName: name}))
}

func (m *Model) providerAddScreen(s *formState) screen {
	return formScreen(fkProviderAdd, s, huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Title("Provider domain").Description(helpProviderDomain).
				Value(&s.provider).
				Validate(func(host string) error {
					if err := validHost(host); err != nil {
						return err
					}
					p := m.cfg.SSHProfiles[s.editName]
					norm, _ := sshprofile.NormalizeHost(host)
					for _, h := range p.Providers {
						if h == norm {
							return fmt.Errorf("%s is already a provider of %q", norm, s.editName)
						}
					}
					return nil
				}),
		),
	), "Add Provider: "+s.editName)
}

// openProviderRemove confirms removing a provider host.
func (m *Model) openProviderRemove(name, host string) tea.Cmd {
	return m.push(m.providerRemoveScreen(&formState{editName: name, pick: host}))
}

func (m *Model) providerRemoveScreen(s *formState) screen {
	return formScreen(fkProviderRemove, s, huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().Title(fmt.Sprintf("Remove %s from %q?", s.pick, s.editName)).
				Description("The matching host stanza is removed from the managed ssh config.\nThe last provider of a profile cannot be removed.").
				Value(&s.confirm),
		),
	), "Remove Provider")
}

// openGitCreate builds the create-Git-profile form.
func (m *Model) openGitCreate() tea.Cmd {
	return m.push(m.gitCreateScreen(&formState{}))
}

func (m *Model) gitCreateScreen(s *formState) screen {
	return formScreen(fkGitCreate, s, huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Title("GitHub username").Description(helpGitUsername).
				Value(&s.username).Validate(m.validGitUsername("")),
			huh.NewInput().Title("Email").Description(helpEmail).
				Value(&s.email).Validate(validEmail),
			huh.NewInput().Title("Directory (optional)").Description(helpDir).
				Value(&s.directory).Validate(m.validDirectory()),
		),
	), "New Git profile")
}

// openGitUpdate edits a git profile's email; the username is fixed.
func (m *Model) openGitUpdate(username string) tea.Cmd {
	p, ok := m.cfg.GitProfiles[username]
	if !ok {
		m.setStatus(false, "git profile "+username+" does not exist")
		return m.pop()
	}
	return m.push(m.gitUpdateScreen(&formState{editUser: username, email: p.Email}))
}

func (m *Model) gitUpdateScreen(s *formState) screen {
	return formScreen(fkGitUpdate, s, huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Title("Email").Description(helpEmail).
				Value(&s.email).Validate(validEmail),
		).Title("Identity: "+s.editUser).Description("The username cannot be changed."),
	), "Update Git profile")
}

// openGitDelete confirms deleting a git profile.
func (m *Model) openGitDelete(username string) tea.Cmd {
	return m.push(m.gitDeleteScreen(&formState{editUser: username}))
}

func (m *Model) gitDeleteScreen(s *formState) screen {
	note := "Its directory mappings are removed with it."
	if p := m.cfg.GitProfiles[s.editUser]; len(p.Directories) == 0 {
		note = "This is the global identity; no global identity will be set afterwards."
	}
	return formScreen(fkGitDelete, s, huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().Title(fmt.Sprintf("Delete Git profile %q?", s.editUser)).
				Description(note).
				Value(&s.confirm),
		),
	), "Delete Git profile")
}

// openBothCreate builds the combined onboarding form.
func (m *Model) openBothCreate() tea.Cmd {
	return m.push(m.bothCreateScreen(&formState{provider: "github.com"}))
}

func (m *Model) bothCreateScreen(s *formState) screen {
	return formScreen(fkBothCreate, s, huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Title("GitHub username").Description(helpGitUsername).
				Value(&s.username).Validate(m.validGitUsername("")),
			huh.NewInput().Title("Email").Description(helpEmail).
				Value(&s.email).Validate(validEmail),
			huh.NewInput().Title("SSH provider").Description(helpProvider).
				Value(&s.provider).Validate(validHost),
			huh.NewInput().Title("Host alias (optional)").Description(helpAlias).
				Value(&s.hostAlias).Validate(m.validHostAlias("")),
			huh.NewInput().Title("Directory (optional)").Description(helpDir).
				Value(&s.directory).Validate(m.validDirectory()),
		).Title("Set up everything in one go").Description("This creates your Git identity and an SSH key with gpm's defaults."),
	), "New Profile")
}

// openDirAdd asks for a directory to map onto a git profile.
func (m *Model) openDirAdd(username string) tea.Cmd {
	return m.push(m.dirAddScreen(&formState{editUser: username}))
}

func (m *Model) dirAddScreen(s *formState) screen {
	return formScreen(fkDirAdd, s, huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Title("Directory").
				Description("Repositories under this folder will use "+s.editUser+"'s identity.\nExample: ~/Works/company").
				Value(&s.directory).
				Validate(func(dir string) error {
					abs, err := dirs.Normalize(dir)
					if err != nil {
						return err
					}
					if owner, ok := dirs.Owner(m.cfg, abs); ok && owner != s.editUser {
						return fmt.Errorf("%s is already mapped to %q", dirs.Shorten(abs), owner)
					}
					return nil
				}),
		),
	), "Add Directory: "+s.editUser)
}

// openDirRemove confirms removing a directory mapping.
func (m *Model) openDirRemove(username, path string) tea.Cmd {
	return m.push(m.dirRemoveScreen(&formState{editUser: username, pick: path}))
}

func (m *Model) dirRemoveScreen(s *formState) screen {
	return formScreen(fkDirRemove, s, huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().Title(fmt.Sprintf("Remove mapping %s?", dirs.Shorten(s.pick))).
				Description("If this is the profile's last directory it becomes the global\nidentity, which is refused while another profile is global.").
				Value(&s.confirm),
		),
	), "Remove Directory")
}

// hasKeyOnDisk guards key-dependent actions.
func (m *Model) hasKeyOnDisk(name string) bool {
	p, ok := m.cfg.SSHProfiles[name]
	return ok && p.KeyPath != "" && sshkey.Exists(p.KeyPath)
}
