package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/tonmoydeb/gpm/internal/auth"
	"github.com/tonmoydeb/gpm/internal/config"
	"github.com/tonmoydeb/gpm/internal/dirs"
	"github.com/tonmoydeb/gpm/internal/sshkey"
	"github.com/tonmoydeb/gpm/internal/sshprofile"
	"github.com/tonmoydeb/gpm/internal/sync"
)

func newKeyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "key",
		Aliases: []string{"keys"},
		Short:   "Manage SSH keys of SSH identities",
	}
	cmd.AddCommand(
		newKeyGenerateCmd(),
		newKeyListCmd(),
		newKeyShowCmd(),
		newKeyCopyCmd(),
		newKeyRemoveCmd(),
		newKeyTestCmd(),
	)
	return cmd
}

func newKeyTestCmd() *cobra.Command {
	var provider string
	cmd := &cobra.Command{
		Use:   "test <name>",
		Short: "Test SSH authentication for an SSH identity",
		Long: `Run a live SSH authentication test for the identity's key. The
identity's first provider is tested unless --provider names another.
A GitHub test reports the account the key belongs to.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			p, ok, err := sshprofile.Get(cfg, name)
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("ssh profile %q does not exist", name)
			}
			if p.KeyPath == "" || !sshkey.Exists(p.KeyPath) {
				return fmt.Errorf("ssh profile %q has no key on disk; generate one with `gpm key generate %s`", name, name)
			}
			host := provider
			if host == "" {
				if len(p.Providers) == 0 {
					return fmt.Errorf("ssh profile %q has no providers", name)
				}
				host = p.Providers[0]
			}
			fmt.Printf("Testing %s with %s ...\n", host, dirs.Shorten(p.KeyPath))
			res, err := auth.Test("git@"+host, p.KeyPath)
			if err != nil {
				return fmt.Errorf("%s rejected the key of %q: %w", host, name, err)
			}
			if res.Username != "" {
				fmt.Printf("Authenticated as %s (key %s works).\n", res.Username, dirs.Shorten(p.KeyPath))
			} else {
				fmt.Printf("Authenticated against %s (key %s works).\n", host, dirs.Shorten(p.KeyPath))
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&provider, "provider", "", "provider hostname to test (default: the identity's first provider)")
	return cmd
}

func newKeyGenerateCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "generate <name>",
		Short: "Generate an ED25519 key pair for an SSH identity",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			p, ok, err := sshprofile.Get(cfg, name)
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("ssh profile %q does not exist", name)
			}
			path := p.KeyPath
			if path == "" {
				path, err = sshprofile.DefaultKeyPath(name)
				if err != nil {
					return err
				}
			}
			if sshkey.Exists(path) && !force {
				return fmt.Errorf("key %s already exists (use --force to regenerate)", path)
			}
			// The username is the key comment.
			pub, err := sshkey.Generate(path, p.Username)
			if err != nil {
				return err
			}
			if p.KeyPath != path {
				if err := sshprofile.Edit(cfg, name, func(pp *config.SSHProfile) { pp.KeyPath = path }); err != nil {
					return err
				}
				if err := sync.All(cfg); err != nil {
					return err
				}
			}
			fmt.Printf("Generated ED25519 key pair for ssh profile %q:\n", name)
			fmt.Printf("  private: %s\n", path)
			fmt.Printf("  public:  %s.pub\n", path)
			fmt.Printf("\n%s", pub)
			fmt.Printf("\nAdd it to your provider account with: gpm key copy %s\n", name)
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "overwrite an existing key pair")
	return cmd
}

func newKeyListCmd() *cobra.Command {
	var all bool
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List SSH keys",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
			fmt.Fprintln(w, "PROFILE\tKEY\tSTATUS\tFINGERPRINT")
			referenced := map[string]bool{}
			for _, name := range sshprofile.SortedUsernames(cfg) {
				p := cfg.SSHProfiles[name]
				status, fp := "missing", "-"
				if p.KeyPath == "" {
					status = "none"
				} else {
					referenced[p.KeyPath] = true
					referenced[p.KeyPath+".pub"] = true
					if sshkey.Exists(p.KeyPath) {
						status = "ok"
						if f, err := sshkey.Fingerprint(sshkey.PubPath(p.KeyPath)); err == nil {
							fp = f
						}
					}
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", name, p.KeyPath, status, fp)
			}
			if all {
				for _, extra := range unmanagedPubKeys(referenced) {
					fp, err := sshkey.Fingerprint(extra)
					if err != nil {
						fp = "-"
					}
					fmt.Fprintf(w, "-\t%s\tunmanaged\t%s\n", extra, fp)
				}
			}
			return w.Flush()
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "also list public keys in ~/.ssh that gpm does not manage")
	return cmd
}

// unmanagedPubKeys lists *.pub files in ~/.ssh not referenced by profiles.
func unmanagedPubKeys(referenced map[string]bool) []string {
	sshDir, err := config.SSHDir()
	if err != nil {
		return nil
	}
	matches, err := filepath.Glob(filepath.Join(sshDir, "*.pub"))
	if err != nil {
		return nil
	}
	var out []string
	for _, m := range matches {
		if !referenced[m] {
			out = append(out, m)
		}
	}
	return out
}

func newKeyShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <name>",
		Short: "Print the public key of an SSH identity",
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
			if p.KeyPath == "" {
				return fmt.Errorf("ssh profile %q has no key; generate one with `gpm key generate %s`", args[0], args[0])
			}
			pub, err := sshkey.PublicKey(p.KeyPath)
			if err != nil {
				return err
			}
			fmt.Print(pub)
			return nil
		},
	}
}

func newKeyCopyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "copy <name>",
		Short: "Copy the public key of an SSH identity to the clipboard",
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
			if p.KeyPath == "" {
				return fmt.Errorf("ssh profile %q has no key; generate one with `gpm key generate %s`", args[0], args[0])
			}
			pub, err := sshkey.PublicKey(p.KeyPath)
			if err != nil {
				return err
			}
			if err := clipboard.WriteAll(strings.TrimSpace(pub)); err != nil {
				fmt.Print(pub)
				return fmt.Errorf("clipboard unavailable (%v); public key printed instead", err)
			}
			fmt.Printf("Public key of %q copied to clipboard.\n", args[0])
			return nil
		},
	}
}

func newKeyRemoveCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "remove <name>",
		Short: "Delete the key pair of an SSH identity",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			p, ok, err := sshprofile.Get(cfg, name)
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("ssh profile %q does not exist", name)
			}
			if p.KeyPath == "" {
				return fmt.Errorf("ssh profile %q has no key", name)
			}
			if !force && interactive() {
				confirm := false
				if err := huh.NewForm(huh.NewGroup(
					huh.NewConfirm().Title(fmt.Sprintf("Delete key pair %s(.pub)?", p.KeyPath)).Value(&confirm),
				)).Run(); err != nil {
					return err
				}
				if !confirm {
					fmt.Println("Aborted.")
					return nil
				}
			} else if !force {
				return fmt.Errorf("refusing to delete %s without --force or an interactive prompt", p.KeyPath)
			}
			if err := sshkey.Remove(p.KeyPath); err != nil {
				return err
			}
			if err := sshprofile.Edit(cfg, name, func(pp *config.SSHProfile) { pp.KeyPath = "" }); err != nil {
				return err
			}
			if err := sync.All(cfg); err != nil {
				return err
			}
			fmt.Printf("Key pair of ssh profile %q deleted.\n", name)
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "delete without prompting")
	return cmd
}
