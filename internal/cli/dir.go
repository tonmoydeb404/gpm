package cli

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/tonmoydeb404/gpm/internal/dirs"
	"github.com/tonmoydeb404/gpm/internal/sync"
)

func newDirCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "dir",
		Aliases: []string{"dirs", "directory"},
		Short:   "Manage directory → profile mappings",
	}
	cmd.AddCommand(
		newDirAddCmd(),
		newDirRemoveCmd(),
		newDirListCmd(),
	)
	return cmd
}

func newDirAddCmd() *cobra.Command {
	var force bool
	cmd := &cobra.Command{
		Use:   "add [path] <username>",
		Short: "Map a directory to a Git identity",
		Long: `Map a directory to a Git identity. With a single argument the current
working directory is mapped.

Examples:
  gpm dir add ~/Works/corp janedoe
  gpm dir add oss            # map the current directory`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			path, username := ".", args[0]
			if len(args) == 2 {
				path, username = args[0], args[1]
			}
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			res, err := dirs.Add(cfg, path, username, force)
			if err != nil {
				return err
			}
			if err := sync.All(cfg); err != nil {
				return err
			}
			if res.Moved || res.Replaced {
				fmt.Printf("Remapped %s to %q.\n", dirs.Shorten(res.Path), username)
			} else {
				fmt.Printf("Mapped %s to %q.\n", dirs.Shorten(res.Path), username)
			}
			for _, note := range res.Notes {
				fmt.Println("  " + note)
			}
			fmt.Printf("Repos under this directory now use the %q identity (includeIf applied on next git command).\n", username)
			return nil
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "take the path away from another profile")
	return cmd
}

func newDirRemoveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "remove <path>",
		Aliases: []string{"rm", "delete"},
		Short:   "Remove a directory mapping",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			removed, err := dirs.Remove(cfg, args[0])
			if err != nil {
				return err
			}
			if err := sync.All(cfg); err != nil {
				return err
			}
			fmt.Printf("Removed mapping %s → %q.\n", dirs.Shorten(removed.Path), removed.Profile)
			if len(cfg.GitProfiles[removed.Profile].Directories) == 0 {
				fmt.Printf("%q is now the global identity.\n", removed.Profile)
			}
			return nil
		},
	}
	return cmd
}

func newDirListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List directory mappings",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			mappings := dirs.All(cfg)
			if len(mappings) == 0 {
				fmt.Println("No directory mappings yet. Add one with: gpm dir add <path> <username>")
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
			fmt.Fprintln(w, "DIRECTORY\tUSERNAME")
			for _, m := range mappings {
				fmt.Fprintf(w, "%s\t%s\n", dirs.Shorten(m.Path), m.Profile)
			}
			return w.Flush()
		},
	}
}
