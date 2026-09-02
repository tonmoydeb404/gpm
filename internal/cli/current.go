package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/tonmoydeb/gpm/internal/config"
	"github.com/tonmoydeb/gpm/internal/dirs"
	"github.com/tonmoydeb/gpm/internal/gitcmd"
	"github.com/tonmoydeb/gpm/internal/resolve"
	"github.com/tonmoydeb/gpm/internal/sync"
)

func newCurrentCmd() *cobra.Command {
	var dir string
	var noCheck bool
	cmd := &cobra.Command{
		Use:   "current",
		Short: "Show the active profile for the current directory",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			if dir == "" {
				dir, err = os.Getwd()
				if err != nil {
					return fmt.Errorf("resolve working directory: %w", err)
				}
			}
			absDir, err := dirs.Normalize(dir)
			if err != nil {
				return err
			}

			name, how, err := resolveProfileForDir(cfg, absDir)
			if err != nil {
				return err
			}
			fmt.Printf("Directory: %s\n", dirs.Shorten(absDir))
			if name == "" {
				fmt.Println("Profile:   (none — no mapping matches and no global profile is set)")
				return nil
			}
			fmt.Printf("Profile:   %s (%s)\n", name, how)

			p := cfg.GitProfiles[name]
			fmt.Printf("git name:  %s\n", p.Username)
			fmt.Printf("git email: %s\n", p.Email)

			if noCheck || !gitcmd.InsideWorkTree(absDir) {
				return nil
			}
			actualEmail, err := gitcmd.ConfigValue(absDir, "user.email")
			if err != nil {
				return err
			}
			switch {
			case actualEmail == "":
				fmt.Println("Identity:  this repo has no local identity yet")
				fmt.Printf("Run:       gpm apply %s\n", name)
			case actualEmail == p.Email:
				fmt.Println("Identity:  OK (matches git config)")
			default:
				fmt.Printf("Identity:  MISMATCH — git config says %q\n", actualEmail)
				fmt.Printf("Run:       gpm apply %s\n", name)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&dir, "dir", "", "resolve for this directory instead of the working directory")
	cmd.Flags().BoolVar(&noCheck, "no-check", false, "skip the git identity cross-check")
	return cmd
}

// resolveProfileForDir delegates to the shared resolution rules.
func resolveProfileForDir(cfg *config.Config, absDir string) (name, how string, err error) {
	return resolve.Profile(cfg, absDir)
}

func newApplyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "apply [profile]",
		Short: "Apply a profile's identity to the current repository",
		Long: `Write user.name and user.email to the local config of the git
repository containing the current directory. Without an argument the
profile is resolved from directory mappings and the default profile.`,
		Args: cobra.RangeArgs(0, 1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("resolve working directory: %w", err)
			}
			var name string
			if len(args) == 1 {
				name = args[0]
				if _, ok := cfg.GitProfiles[name]; !ok {
					return fmt.Errorf("git profile %q does not exist", name)
				}
			} else {
				name, _, err = resolveProfileForDir(cfg, cwd)
				if err != nil {
					return err
				}
				if name == "" {
					return fmt.Errorf("no profile resolved for %s; pass one explicitly: gpm apply <username>", dirs.Shorten(cwd))
				}
			}
			if !gitcmd.InsideWorkTree(cwd) {
				return fmt.Errorf("%s is not inside a git work tree", dirs.Shorten(cwd))
			}
			root, err := gitcmd.TopLevel(cwd)
			if err != nil {
				return err
			}
			p := cfg.GitProfiles[name]
			if err := gitcmd.SetIdentity(cwd, p.Username, p.Email); err != nil {
				return err
			}
			fmt.Printf("Applied %q to %s\n", name, dirs.Shorten(filepath.Clean(root)))
			fmt.Printf("  git name:  %s\n", p.Username)
			fmt.Printf("  git email: %s\n", p.Email)
			return nil
		},
	}
	return cmd
}

func newSyncCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "sync",
		Short: "Regenerate all managed config from gpm's state",
		Long: `Regenerate the managed block in ~/.ssh/config, the per-profile
gitconfig files, and the includeIf block in the global gitconfig.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			if err := sync.All(cfg); err != nil {
				return err
			}
			fmt.Printf("Synced %d git profile(s), %d ssh profile(s) and %d mapping(s).\n",
				len(cfg.GitProfiles), len(cfg.SSHProfiles), dirs.Count(cfg))
			return nil
		},
	}
}
