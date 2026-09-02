package cli

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/tonmoydeb/gpm/internal/config"
	"github.com/tonmoydeb/gpm/internal/dirs"
	"github.com/tonmoydeb/gpm/internal/gitprofile"
	"github.com/tonmoydeb/gpm/internal/sync"
)

func newProfileCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "profile",
		Aliases: []string{"profiles"},
		Short:   "Manage Git identities",
	}
	cmd.AddCommand(
		newProfileAddCmd(),
		newProfileListCmd(),
		newProfileShowCmd(),
		newProfileEditCmd(),
		newProfileRemoveCmd(),
	)
	return cmd
}

func newProfileAddCmd() *cobra.Command {
	var (
		email string
		dir   string
	)
	cmd := &cobra.Command{
		Use:   "add <username>",
		Short: "Create a new Git identity",
		Long: `Create a new Git identity. The username is the commit author name
and identifies the profile.

Examples:
  gpm profile add janedoe --email jane@corp.com
  gpm profile add janedoe --email jane@corp.com --dir ~/Works/corp
  gpm profile add janedoe --email jane@corp.com --dir -   # global identity`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			username := args[0]
			if email == "" {
				if !interactive() {
					return fmt.Errorf("--email is required when not attached to a terminal")
				}
				if err := huh.NewForm(huh.NewGroup(
					huh.NewInput().Title("Git user.email").Value(&email).
						Validate(func(s string) error {
							if !containsAt(s) {
								return fmt.Errorf("must be a valid email")
							}
							return nil
						}),
				)).Run(); err != nil {
					return err
				}
			}
			if dir == "-" {
				dir = "" // explicit global
			}

			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			res, err := gitprofile.Add(cfg, config.GitProfile{
				Username: username,
				Email:    email,
			}, dir)
			if err != nil {
				return err
			}
			if err := sync.All(cfg); err != nil {
				return err
			}

			fmt.Printf("Git identity %q created.\n\n", username)
			fmt.Printf("  git name:  %s\n", username)
			fmt.Printf("  git email: %s\n", email)
			switch {
			case res.IsGlobal:
				fmt.Printf("  scope:     global (applies where no directory mapping matches)\n")
			case dir != "":
				fmt.Printf("  directory: %s\n", dirs.Shorten(mustExpand(dir)))
			}
			fmt.Printf("\nClone and commit in mapped directories to use this identity.\n")
			return nil
		},
	}
	cmd.Flags().StringVar(&email, "email", "", "git user.email for the profile")
	cmd.Flags().StringVar(&dir, "dir", "", `directory mapped to this identity ("-" for the global identity)`)
	return cmd
}

func mustExpand(p string) string {
	abs, err := dirs.Normalize(p)
	if err != nil {
		return p
	}
	return abs
}

func containsAt(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == '@' {
			return true
		}
	}
	return false
}

func newProfileListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List Git identities",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			usernames := gitprofile.SortedUsernames(cfg)
			if len(usernames) == 0 {
				fmt.Println("No Git identities yet. Create one with: gpm profile add <username> --email ...")
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
			fmt.Fprintln(w, "USERNAME\tEMAIL\tDIRECTORIES\tSCOPE")
			for _, name := range usernames {
				p := cfg.GitProfiles[name]
				scope := ""
				if len(p.Directories) == 0 {
					scope = "* global"
				}
				fmt.Fprintf(w, "%s\t%s\t%d\t%s\n", p.Username, p.Email, len(p.Directories), scope)
			}
			return w.Flush()
		},
	}
}

func newProfileShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <username>",
		Short: "Show details of a Git identity",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			p, ok, err := gitprofile.Get(cfg, args[0])
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("git profile %q does not exist", args[0])
			}
			fmt.Printf("Username:  %s\n", p.Username)
			if len(p.Directories) == 0 {
				fmt.Printf("Scope:     global\n")
			}
			fmt.Printf("Email:     %s\n", p.Email)
			if len(p.Directories) > 0 {
				fmt.Printf("\nDirectories:\n")
				for _, d := range p.Directories {
					fmt.Printf("  %s\n", dirs.Shorten(d))
				}
			}
			return nil
		},
	}
}

func newProfileEditCmd() *cobra.Command {
	var email string
	cmd := &cobra.Command{
		Use:   "edit <username>",
		Short: "Edit a Git identity",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			username := args[0]
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			current, ok, err := gitprofile.Get(cfg, username)
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("git profile %q does not exist", username)
			}

			prompted := false
			if interactive() && cmd.Flags().NFlag() == 0 {
				email = current.Email
				if err := huh.NewForm(huh.NewGroup(
					huh.NewInput().Title("Git user.email").Value(&email),
				)).Run(); err != nil {
					return err
				}
				prompted = true
			}

			err = gitprofile.Edit(cfg, username, func(p *config.GitProfile) {
				if cmd.Flags().Changed("email") || (prompted && email != "") {
					p.Email = email
				}
			})
			if err != nil {
				return err
			}
			if err := sync.All(cfg); err != nil {
				return err
			}
			fmt.Printf("Git identity %q updated.\n", username)
			return nil
		},
	}
	cmd.Flags().StringVar(&email, "email", "", "new git user.email")
	return cmd
}

func newProfileRemoveCmd() *cobra.Command {
	var assumeYes bool
	cmd := &cobra.Command{
		Use:     "remove <username>",
		Aliases: []string{"rm", "delete"},
		Short:   "Delete a Git identity",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			username := args[0]
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			if _, ok := cfg.GitProfiles[username]; !ok {
				return fmt.Errorf("git profile %q does not exist", username)
			}
			if !assumeYes && interactive() {
				confirm := false
				if err := huh.NewForm(huh.NewGroup(
					huh.NewConfirm().Title(fmt.Sprintf("Delete git profile %q?", username)).
						Description("Its directory mappings are removed with it."),
				)).Run(); err != nil {
					return err
				}
				if !confirm {
					fmt.Println("Aborted.")
					return nil
				}
			}
			res, err := gitprofile.Remove(cfg, username)
			if err != nil {
				return err
			}
			if err := sync.All(cfg); err != nil {
				return err
			}
			fmt.Printf("Git profile %q deleted.\n", username)
			if res.RemovedDirs > 0 {
				fmt.Printf("%d directory mapping(s) removed.\n", res.RemovedDirs)
			}
			if res.WasGlobal {
				fmt.Println("This was the global profile; no global identity is set now.")
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&assumeYes, "yes", "y", false, "skip the confirmation prompt")
	return cmd
}
