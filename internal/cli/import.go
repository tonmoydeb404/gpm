package cli

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/tonmoydeb404/gpm/internal/dirs"
	"github.com/tonmoydeb404/gpm/internal/importer"
	"github.com/tonmoydeb404/gpm/internal/sync"
)

func newImportCmd() *cobra.Command {
	var dryRun, assumeYes bool
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Import an existing Git/SSH multi-account setup into gpm",
		Long: `Scan the machine — Host stanzas with identity files in ~/.ssh/config
and includeIf rules across the global gitconfig's include chain — then
propose gpm profiles for them. Nothing is written before you confirm.
Run gpm scan for a read-only preview. Bare Host github.com becomes the
global SSH identity.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			rep := importer.Scan(cfg, importer.ScanOptions{})
			for _, w := range rep.Warnings {
				fmt.Println("note:", w)
			}
			fresh := rep.Fresh(cfg)
			for _, c := range rep.Candidates {
				if !containsCandidate(fresh, c.Username) {
					fmt.Printf("skip %q: ssh profile already exists\n", c.Username)
				}
			}
			if len(fresh) == 0 {
				fmt.Println("Nothing new to import.")
				return nil
			}

			fmt.Println("\nFound the following accounts:")
			for _, c := range fresh {
				email := c.Email
				if email == "" {
					email = "(email unknown)"
				}
				alias := c.HostAlias
				if alias == "" {
					alias = "(global)"
				}
				fmt.Printf("  %-16s %s [%s] -> %s (%s)\n", c.Username, strings.Join(c.Hosts, ", "), alias, dirs.Shorten(c.KeyPath), email)
				for _, d := range c.Directories {
					fmt.Printf("  %-16s directory: %s\n", "", dirs.Shorten(d))
				}
			}
			if dryRun {
				fmt.Println("\nDry run; nothing written. Re-run without --dry-run to import.")
				return nil
			}

			selected, err := selectCandidates(fresh, assumeYes)
			if err != nil {
				return err
			}
			if len(selected) == 0 {
				fmt.Println("Aborted.")
				return nil
			}

			failed := 0
			for _, c := range selected {
				note, err := importer.Apply(cfg, c)
				if err != nil {
					fmt.Printf("skip %q: %v\n", c.Username, err)
					failed++
					continue
				}
				if note != "" {
					fmt.Printf("  (%s)\n", note)
				}
				alias := c.HostAlias
				if alias == "" {
					alias = "(global)"
				}
				fmt.Printf("Imported %q [%s] (%s)\n", c.Username, alias, dirs.Shorten(c.KeyPath))
			}
			if err := sync.All(cfg); err != nil {
				return err
			}
			if failed == len(selected) {
				return fmt.Errorf("nothing was imported")
			}
			fmt.Printf("\nDone. Verify with: gpm doctor --network\n")
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "only show what would be imported")
	cmd.Flags().BoolVarP(&assumeYes, "yes", "y", false, "import all detected accounts without prompting")
	return cmd
}

// containsCandidate reports whether the list has a candidate with
// that username.
func containsCandidate(list []importer.Candidate, username string) bool {
	for _, c := range list {
		if c.Username == username {
			return true
		}
	}
	return false
}

// selectCandidates asks the user which candidates to import.
func selectCandidates(fresh []importer.Candidate, assumeYes bool) ([]importer.Candidate, error) {
	// Non-interactive: need --yes, and only candidates with a known email.
	if !interactive() {
		if !assumeYes {
			return nil, fmt.Errorf("attach a terminal for an interactive import, or pass --yes to import all detected accounts")
		}
		var withEmail []importer.Candidate
		for _, c := range fresh {
			if c.Email == "" {
				fmt.Printf("skip %q: email unknown (add it later with: gpm profile add %s --email ...)\n", c.Username, c.Username)
				continue
			}
			withEmail = append(withEmail, c)
		}
		return withEmail, nil
	}

	// Fill in unknown emails interactively.
	for i := range fresh {
		if fresh[i].Email != "" {
			continue
		}
		if err := huh.NewForm(huh.NewGroup(
			huh.NewInput().Title(fmt.Sprintf("Email for %q", fresh[i].Username)).
				Value(&fresh[i].Email).
				Validate(func(s string) error {
					if !containsAt(s) {
						return fmt.Errorf("must be a valid email")
					}
					return nil
				}),
		)).Run(); err != nil {
			return nil, err
		}
	}

	options := make([]huh.Option[int], 0, len(fresh))
	for i, c := range fresh {
		alias := c.HostAlias
		if alias == "" {
			alias = "global"
		}
		options = append(options, huh.Option[int]{
			Key:   fmt.Sprintf("%s [%s] (%s)", c.Username, alias, c.Email),
			Value: i,
		}.Selected(true))
	}
	var picked []int
	if err := huh.NewForm(huh.NewGroup(
		huh.NewMultiSelect[int]().Title("Accounts to import").Options(options...).Value(&picked),
	)).Run(); err != nil {
		return nil, err
	}
	out := make([]importer.Candidate, 0, len(picked))
	for _, i := range picked {
		out = append(out, fresh[i])
	}
	return out, nil
}
