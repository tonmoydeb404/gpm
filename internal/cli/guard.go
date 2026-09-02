package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/tonmoydeb/gpm/internal/config"
	"github.com/tonmoydeb/gpm/internal/dirs"
	"github.com/tonmoydeb/gpm/internal/gitcmd"
	"github.com/tonmoydeb/gpm/internal/guard"
	"github.com/tonmoydeb/gpm/internal/resolve"
)

func newGuardCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "guard",
		Short: "Protect repos from committing with the wrong identity",
		Long: `Install a pre-commit hook that verifies the repo identity matches
the profile mapped to its directory. Unmapped directories are never
blocked.`,
	}
	cmd.AddCommand(
		newGuardInstallCmd(),
		newGuardRemoveCmd(),
	)
	return cmd
}

func newGuardInstallCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "install [path]",
		Short: "Install the pre-commit guard hook in a repository",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := targetDir(args)
			if err != nil {
				return err
			}
			path, err := guard.Install(dir, force)
			if err != nil {
				return err
			}
			fmt.Printf("Guard installed: %s\n", path)
			fmt.Println("Commits with an identity that does not match the mapped profile will be blocked.")
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "replace an existing non-gpm pre-commit hook")
	return cmd
}

func newGuardRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "remove [path]",
		Short: "Remove the gpm guard hook from a repository",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := targetDir(args)
			if err != nil {
				return err
			}
			removed, err := guard.Remove(dir)
			if err != nil {
				return err
			}
			if removed {
				fmt.Println("Guard removed.")
			} else {
				fmt.Println("No guard hook was installed.")
			}
			return nil
		},
	}
}

// targetDir resolves the optional path argument (default: cwd).
func targetDir(args []string) (string, error) {
	if len(args) == 1 {
		return dirs.Normalize(args[0])
	}
	return os.Getwd()
}

// newPrecommitCmd is invoked by the installed git hook, not by users.
func newPrecommitCmd() *cobra.Command {
	return &cobra.Command{
		Use:    "precommit",
		Short:  "Internal: pre-commit identity check (invoked by the guard hook)",
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cwd, err := os.Getwd()
			if err != nil {
				return err
			}
			// Not a repo, or no profile governs this directory: allow.
			if !gitcmd.InsideWorkTree(cwd) {
				return nil
			}
			cfg, err := config.Load()
			if err != nil {
				return err // fail-open is wrong for corrupt config; report it
			}
			name, _, err := resolve.Profile(cfg, cwd)
			if err != nil {
				return err
			}
			if name == "" {
				return nil // unmapped directories are unguarded
			}
			p := cfg.GitProfiles[name]
			email, err := gitcmd.ConfigValue(cwd, "user.email")
			if err != nil {
				return err
			}
			if email == "" {
				// git itself will refuse or fall back; nothing to compare.
				return nil
			}
			if email != p.Email {
				return fmt.Errorf(
					"commit blocked — repo identity is %q but directory maps to profile %q (%s)\n"+
						"  fix:  gpm apply %s\n"+
						"  or:   git commit --no-verify to override",
					email, name, p.Email, name)
			}
			return nil
		},
	}
}
