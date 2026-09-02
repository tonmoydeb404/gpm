package cli

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/tonmoydeb/gpm/internal/config"
	"github.com/tonmoydeb/gpm/internal/dirs"
	"github.com/tonmoydeb/gpm/internal/sshconfig"
	"github.com/tonmoydeb/gpm/internal/sshkey"
	"github.com/tonmoydeb/gpm/internal/sshprofile"
	"github.com/tonmoydeb/gpm/internal/sync"
)

func newSSHCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "ssh",
		Aliases: []string{"key-profile", "ssh-profile"},
		Short:   "Manage SSH identities",
	}
	cmd.AddCommand(
		newSSHAddCmd(),
		newSSHListCmd(),
		newSSHShowCmd(),
		newSSHRemoveCmd(),
		newSSHProviderCmd(),
	)
	return cmd
}

func newSSHAddCmd() *cobra.Command {
	var (
		provider string
		alias    string
		keyPath  string
	)
	cmd := &cobra.Command{
		Use:   "add <username>",
		Short: "Create a new SSH identity and generate its key",
		Long: `Create a new SSH identity. An ED25519 key pair is generated at the
conventional location unless an existing key is passed with --key.
The username is embedded as the key comment.

With no --alias the profile becomes the global SSH identity; only one
profile can be global at a time.

Examples:
  gpm ssh add work --provider github.com --alias gpm-work
  gpm ssh add work --provider github.com
  gpm ssh add legacy --provider github.com --alias legacy --key ~/.ssh/id_old`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			username := args[0]
			if provider == "" {
				if !interactive() {
					return fmt.Errorf("--provider is required when not attached to a terminal")
				}
				if err := huh.NewForm(huh.NewGroup(
					huh.NewInput().Title("Provider hostname").
						Description("The Git provider this key authenticates with (e.g. github.com)").
						Value(&provider).
						Validate(func(s string) error {
							_, err := sshprofile.NormalizeHost(s)
							return err
						}),
				)).Run(); err != nil {
					return err
				}
			}

			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			res, err := sshprofile.Add(cfg, config.SSHProfile{
				Username:  username,
				HostAlias: alias,
				KeyPath:   keyPath,
				Providers: []string{provider},
			}, sshprofile.AddOpts{GenerateKey: keyPath == ""})
			if err != nil {
				return err
			}
			if err := sync.All(cfg); err != nil {
				return err
			}

			p := cfg.SSHProfiles[username]
			fmt.Printf("SSH profile %q created.\n\n", username)
			fmt.Printf("  provider:  %s\n", strings.Join(p.Providers, ", "))
			if p.HostAlias == "" {
				fmt.Printf("  scope:     global (no host alias; not wired into ~/.ssh/config)\n")
			} else {
				fmt.Printf("  alias:     %s\n", p.HostAlias)
			}
			fmt.Printf("  key:       %s\n", dirs.Shorten(p.KeyPath))
			if res.GeneratedKey {
				fmt.Printf("\nPublic key (add it to your provider account):\n\n%s", res.PublicKey)
				fmt.Printf("Copy it with: gpm key copy %s\n", username)
				fmt.Printf("Verify later with: gpm key test %s\n", username)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&provider, "provider", "", "provider hostname this key authenticates with (e.g. github.com)")
	cmd.Flags().StringVar(&alias, "alias", "", `ssh config host alias (omit for the global identity)`)
	cmd.Flags().StringVar(&keyPath, "key", "", "path to an existing private key (skips generation)")
	return cmd
}

func newSSHListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List SSH identities",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			names := sshprofile.SortedUsernames(cfg)
			if len(names) == 0 {
				fmt.Println("No SSH identities yet. Create one with: gpm ssh add <name> --provider github.com")
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
			fmt.Fprintln(w, "USERNAME\tALIAS\tPROVIDERS\tKEY\tSTATUS")
			for _, username := range sshprofile.SortedUsernames(cfg) {
				p := cfg.SSHProfiles[username]
				status := "missing"
				if p.KeyPath == "" {
					status = "none"
				} else if sshkey.Exists(p.KeyPath) {
					status = "ok"
				}
				alias := p.HostAlias
				if alias == "" {
					alias = "* global"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", username, alias, strings.Join(p.Providers, ", "), p.KeyPath, status)
			}
			return w.Flush()
		},
	}
}

func newSSHShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <name>",
		Short: "Show details of an SSH identity",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			p, ok, err := sshprofile.Get(cfg, args[0])
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("ssh profile %q does not exist", args[0])
			}
			fmt.Printf("Username:  %s\n", p.Username)
			if p.HostAlias == "" {
				fmt.Printf("Alias:     (none — global identity)\n")
			} else {
				fmt.Printf("Alias:     %s\n", p.HostAlias)
			}
			fmt.Printf("Providers: %s\n", strings.Join(p.Providers, ", "))
			fmt.Printf("Key:       %s\n", p.KeyPath)
			if p.KeyPath != "" && sshkey.Exists(p.KeyPath) {
				if fp, err := sshkey.Fingerprint(sshkey.PubPath(p.KeyPath)); err == nil {
					fmt.Printf("Fingerprint: %s\n", fp)
				}
			}
			stanzas := sshconfig.Stanzas(cfg)
			var mine []sshconfig.Host
			for _, h := range stanzas {
				if h.IdentityFile == p.KeyPath {
					mine = append(mine, h)
				}
			}
			if len(mine) > 0 {
				fmt.Printf("\nSSH host stanzas (managed in ~/.ssh/config):\n")
				for _, h := range mine {
					fmt.Printf("  Host %s -> %s (User %s)\n", h.Alias, h.HostName, h.User)
					fmt.Printf("    clone: git clone git@%s:<owner>/<repo>.git\n", h.Alias)
				}
			}
			return nil
		},
	}
}

func newSSHRemoveCmd() *cobra.Command {
	var removeKey, assumeYes bool
	cmd := &cobra.Command{
		Use:     "remove <name>",
		Aliases: []string{"rm", "delete"},
		Short:   "Delete an SSH identity",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			if _, ok := cfg.SSHProfiles[name]; !ok {
				return fmt.Errorf("ssh profile %q does not exist", name)
			}
			if !assumeYes && interactive() {
				confirm := false
				if err := huh.NewForm(huh.NewGroup(
					huh.NewConfirm().Title(fmt.Sprintf("Delete ssh profile %q?", name)).
						Description("The managed SSH config entries are regenerated."),
				)).Run(); err != nil {
					return err
				}
				if !confirm {
					fmt.Println("Aborted.")
					return nil
				}
			}
			res, err := sshprofile.Remove(cfg, name, sshprofile.RemoveOpts{RemoveKey: removeKey})
			if err != nil {
				return err
			}
			if err := sync.All(cfg); err != nil {
				return err
			}
			fmt.Printf("SSH profile %q deleted.\n", name)
			if res.RemovedKeyPath != "" {
				fmt.Printf("Key pair removed: %s(.pub)\n", res.RemovedKeyPath)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&removeKey, "remove-key", false, "also delete the SSH key pair from disk")
	cmd.Flags().BoolVarP(&assumeYes, "yes", "y", false, "skip the confirmation prompt")
	return cmd
}

func newSSHProviderCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "provider",
		Short: "Manage the provider hostnames of an SSH identity",
	}
	cmd.AddCommand(
		newSSHProviderAddCmd(),
		newSSHProviderRemoveCmd(),
	)
	return cmd
}

func newSSHProviderAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "add <name> <host>",
		Short:   "Attach a provider hostname to an SSH identity",
		Example: `  gpm ssh provider add work git.company.com`,
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			if err := sshprofile.AddProvider(cfg, args[0], args[1]); err != nil {
				return err
			}
			if err := sync.All(cfg); err != nil {
				return err
			}
			fmt.Printf("%s is now a provider of %q.\n", strings.ToLower(args[1]), args[0])
			return nil
		},
	}
}

func newSSHProviderRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "remove <name> <host>",
		Aliases: []string{"rm"},
		Short:   "Detach a provider hostname from an SSH identity",
		Example: `  gpm ssh provider remove work git.company.com`,
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			if err := sshprofile.RemoveProvider(cfg, args[0], args[1]); err != nil {
				return err
			}
			if err := sync.All(cfg); err != nil {
				return err
			}
			fmt.Printf("%s removed from %q.\n", strings.ToLower(args[1]), args[0])
			return nil
		},
	}
}
