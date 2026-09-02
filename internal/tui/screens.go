package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/tonmoydeb/gpm/internal/config"
	"github.com/tonmoydeb/gpm/internal/dirs"
	"github.com/tonmoydeb/gpm/internal/gitprofile"
	"github.com/tonmoydeb/gpm/internal/sshkey"
	"github.com/tonmoydeb/gpm/internal/sshprofile"
)

// --- screen constructors -----------------------------------------------

// homeScreen is the root of the interface.
func homeScreen() screen {
	return screen{
		kind:  kindMenu,
		title: "GPM",
		items: []menuItem{
			{label: "SSH Profiles", desc: "SSH keys and their providers", action: "home:ssh"},
			{label: "Git Profiles", desc: "commit identities", action: "home:git"},
			{label: "Create", desc: "set up a new profile", action: "home:create"},
			{label: "Run Doctor", desc: "diagnose the gpm setup", action: "home:doctor"},
			{label: "Exit", action: "quit"},
		},
	}
}

// sshListScreen lists existing SSH profiles and the create action.
func sshListScreen(m *Model) screen {
	s := screen{
		kind:  kindMenu,
		title: "SSH Profiles",
		items: []menuItem{
			{label: "Create New", desc: "generate a key for a provider", action: "ssh:create"},
		},
		rebuild: sshListScreen,
	}
	for _, name := range sshprofile.SortedUsernames(m.cfg) {
		p := m.cfg.SSHProfiles[name]
		desc := strings.Join(p.Providers, ", ")
		if p.KeyPath == "" || !sshkey.Exists(p.KeyPath) {
			desc += " · key missing"
		}
		s.items = append(s.items, menuItem{label: name, desc: desc, action: "ssh:open:" + name})
	}
	if len(s.items) == 1 {
		s.items[0].desc = "no SSH profiles yet"
	}
	s.items = append(s.items, menuItem{label: "Back", action: "back"})
	return s
}

// sshDetailScreen is the action menu of one SSH profile.
func sshDetailScreen(m *Model, name string) screen {
	p, ok := m.cfg.SSHProfiles[name]
	if !ok {
		return sshListScreen(m)
	}
	keyState := "✓ key on disk"
	if p.KeyPath == "" || !sshkey.Exists(p.KeyPath) {
		keyState = "key missing"
	}
	return screen{
		kind:  kindMenu,
		title: "SSH Profile: " + name,
		items: []menuItem{
			{label: "Update", desc: "edit the host alias (username cannot change)", action: "ssh:update:" + name},
			{label: "Delete", desc: "remove this profile", action: "ssh:delete:" + name},
			{label: "View Public Key", desc: keyState, action: "ssh:pubkey:" + name},
			{label: "Copy Public Key", desc: "put it on the clipboard", action: "ssh:copy:" + name},
			{label: "Test Connection", desc: "authenticate against " + firstProvider(p), action: "ssh:test:" + name},
			{label: "Providers", desc: strings.Join(p.Providers, ", "), action: "ssh:providers:" + name},
			{label: "Back", action: "back"},
		},
		rebuild: func(m *Model) screen { return sshDetailScreen(m, name) },
	}
}

// providersScreen lists the providers of one SSH profile.
func providersScreen(m *Model, name string) screen {
	p, ok := m.cfg.SSHProfiles[name]
	if !ok {
		return sshListScreen(m)
	}
	s := screen{
		kind:  kindMenu,
		title: "Providers: " + name,
		items: []menuItem{
			{label: "Add Provider", desc: helpProviderDomain, action: "prov:add:" + name},
		},
		rebuild: func(m *Model) screen { return providersScreen(m, name) },
	}
	for _, host := range p.Providers {
		s.items = append(s.items, menuItem{
			label:  host,
			desc:   "select to remove",
			action: "prov:remove:" + name + ":" + host,
		})
	}
	s.items = append(s.items, menuItem{label: "Back", action: "back"})
	return s
}

// gitListScreen lists existing Git profiles and the create action.
func gitListScreen(m *Model) screen {
	s := screen{
		kind:  kindMenu,
		title: "Git Profiles",
		items: []menuItem{
			{label: "Create New", desc: "a name repos commit with", action: "git:create"},
		},
		rebuild: gitListScreen,
	}
	for _, user := range gitprofile.SortedUsernames(m.cfg) {
		p := m.cfg.GitProfiles[user]
		desc := fmt.Sprintf("%s · %d mapping(s)", p.Email, len(p.Directories))
		if len(p.Directories) == 0 {
			desc = p.Email + " · global identity"
		}
		s.items = append(s.items, menuItem{label: user + " <" + p.Email + ">", desc: desc, action: "git:open:" + user})
	}
	if len(s.items) == 1 {
		s.items[0].desc = "no Git profiles yet"
	}
	s.items = append(s.items, menuItem{label: "Back", action: "back"})
	return s
}

// gitDetailScreen is the action menu of one Git profile.
func gitDetailScreen(m *Model, username string) screen {
	p, ok := m.cfg.GitProfiles[username]
	if !ok {
		return gitListScreen(m)
	}
	scope := fmt.Sprintf("%d mapping(s)", len(p.Directories))
	if len(p.Directories) == 0 {
		scope = "global identity"
	}
	return screen{
		kind:  kindMenu,
		title: "Git Profile: " + p.Email,
		items: []menuItem{
			{label: "Update", desc: "edit the email (" + username + " cannot change)", action: "git:update:" + username},
			{label: "Delete", desc: "remove this identity", action: "git:delete:" + username},
			{label: "Directories", desc: scope, action: "git:dirs:" + username},
			{label: "Back", action: "back"},
		},
		rebuild: func(m *Model) screen { return gitDetailScreen(m, username) },
	}
}

// directoriesScreen lists the directory mappings of a git profile.
func directoriesScreen(m *Model, username string) screen {
	p, ok := m.cfg.GitProfiles[username]
	if !ok {
		return gitListScreen(m)
	}
	s := screen{
		kind:  kindMenu,
		title: "Directories: " + username,
		items: []menuItem{
			{label: "Add Directory", desc: "map a folder to this identity", action: "dir:add:" + username},
		},
		rebuild: func(m *Model) screen { return directoriesScreen(m, username) },
	}
	for _, d := range p.Directories {
		s.items = append(s.items, menuItem{
			label:  dirs.Shorten(d),
			desc:   "select to remove",
			action: "dir:remove:" + username + ":" + d,
		})
	}
	s.items = append(s.items, menuItem{label: "Back", action: "back"})
	return s
}

// createMenuScreen offers the three creation flows.
func createMenuScreen() screen {
	return screen{
		kind:  kindMenu,
		title: "Create",
		items: []menuItem{
			{label: "Both Profile", desc: "Git identity + SSH key in one go", action: "create:both"},
			{label: "SSH Profile", desc: "an SSH key for a provider", action: "create:ssh"},
			{label: "Git Profile", desc: "a commit identity", action: "create:git"},
			{label: "Back", action: "back"},
		},
	}
}

// publicKeyScreen shows the public key of an SSH profile.
func publicKeyScreen(m *Model, name string) screen {
	p, ok := m.cfg.SSHProfiles[name]
	if !ok || p.KeyPath == "" || !sshkey.Exists(p.KeyPath) {
		m.setStatus(false, "no key on disk for "+name)
		return sshDetailScreen(m, name)
	}
	pub, err := sshkey.PublicKey(p.KeyPath)
	if err != nil {
		m.setStatus(false, err.Error())
		return sshDetailScreen(m, name)
	}
	content := strings.TrimSpace(pub)
	if fp, err := sshkey.Fingerprint(sshkey.PubPath(p.KeyPath)); err == nil {
		content += "\n\n" + hintStyle.Render("Fingerprint: "+fp)
	}
	return screen{
		kind:     kindText,
		title:    "Public Key: " + name,
		content:  content,
		copyName: name,
	}
}

// firstProvider returns the profile's first provider or a placeholder.
func firstProvider(p config.SSHProfile) string {
	if len(p.Providers) == 0 {
		return "(no provider)"
	}
	return p.Providers[0]
}

// --- action routing ----------------------------------------------------

// runAction turns a menu selection into navigation, forms, or
// mutations.
func (m *Model) runAction(action string) tea.Cmd {
	switch {
	case action == "quit":
		return tea.Quit

	case action == "back":
		return m.pop()

	case action == "home:ssh":
		return m.push(sshListScreen(m))
	case action == "home:git":
		return m.push(gitListScreen(m))
	case action == "home:create":
		return m.push(createMenuScreen())
	case action == "home:doctor":
		return m.runDoctor(false)

	case action == "ssh:create", action == "create:ssh":
		return m.openSSHCreate()
	case action == "git:create", action == "create:git":
		return m.openGitCreate()
	case action == "create:both":
		return m.openBothCreate()

	case strings.HasPrefix(action, "ssh:open:"):
		return m.push(sshDetailScreen(m, strings.TrimPrefix(action, "ssh:open:")))
	case strings.HasPrefix(action, "ssh:update:"):
		return m.openSSHUpdate(strings.TrimPrefix(action, "ssh:update:"))
	case strings.HasPrefix(action, "ssh:delete:"):
		return m.openSSHDelete(strings.TrimPrefix(action, "ssh:delete:"))
	case strings.HasPrefix(action, "ssh:pubkey:"):
		return m.push(publicKeyScreen(m, strings.TrimPrefix(action, "ssh:pubkey:")))
	case strings.HasPrefix(action, "ssh:copy:"):
		m.copyPublicKey(strings.TrimPrefix(action, "ssh:copy:"))
		return nil
	case strings.HasPrefix(action, "ssh:test:"):
		return m.testConnectionFor(strings.TrimPrefix(action, "ssh:test:"))
	case strings.HasPrefix(action, "ssh:providers:"):
		return m.push(providersScreen(m, strings.TrimPrefix(action, "ssh:providers:")))

	case strings.HasPrefix(action, "prov:add:"):
		return m.openProviderAdd(strings.TrimPrefix(action, "prov:add:"))
	case strings.HasPrefix(action, "prov:remove:"):
		name, host, _ := strings.Cut(strings.TrimPrefix(action, "prov:remove:"), ":")
		return m.openProviderRemove(name, host)

	case strings.HasPrefix(action, "git:open:"):
		return m.push(gitDetailScreen(m, strings.TrimPrefix(action, "git:open:")))
	case strings.HasPrefix(action, "git:update:"):
		return m.openGitUpdate(strings.TrimPrefix(action, "git:update:"))
	case strings.HasPrefix(action, "git:delete:"):
		return m.openGitDelete(strings.TrimPrefix(action, "git:delete:"))
	case strings.HasPrefix(action, "git:dirs:"):
		return m.push(directoriesScreen(m, strings.TrimPrefix(action, "git:dirs:")))

	case strings.HasPrefix(action, "dir:add:"):
		return m.openDirAdd(strings.TrimPrefix(action, "dir:add:"))
	case strings.HasPrefix(action, "dir:remove:"):
		user, path, _ := strings.Cut(strings.TrimPrefix(action, "dir:remove:"), ":")
		return m.openDirRemove(user, path)

	case action == "success:copy":
		if m.outcome != nil {
			m.copyPublicKey(m.outcome.SSHName)
		}
		return nil
	case action == "success:test":
		if m.outcome != nil {
			return m.testConnectionFor(m.outcome.SSHName)
		}
		return nil
	case action == "success:done":
		m.stack = m.stack[:1]
		return nil
	}
	return nil
}
