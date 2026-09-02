package cli

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/spf13/cobra"

	"github.com/tonmoydeb/gpm/internal/config"
	"github.com/tonmoydeb/gpm/internal/dirs"
	"github.com/tonmoydeb/gpm/internal/gitprofile"
	"github.com/tonmoydeb/gpm/internal/importer"
	"github.com/tonmoydeb/gpm/internal/sshprofile"
	"github.com/tonmoydeb/gpm/internal/sync"
)

func newImportCmd() *cobra.Command {
	var dryRun, assumeYes bool
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Import an existing Git/SSH multi-account setup into gpm",
		Long: `Detect Host stanzas with identity files in ~/.ssh/config and existing
includeIf rules in the global gitconfig, then propose gpm profiles for
them. Nothing is written before you confirm.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			det, err := importer.Detect(cfg)
			if err != nil {
				return err
			}
			for _, w := range det.Warnings {
				fmt.Println("note:", w)
			}
			var fresh []importer.Candidate
			for _, c := range det.Candidates {
				if _, exists := cfg.SSHProfiles[c.Username]; exists {
					fmt.Printf("skip %q: ssh profile already exists\n", c.Username)
					continue
				}
				fresh = append(fresh, c)
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
				fmt.Printf("  %-16s %s -> %s (%s)\n", c.Username, strings.Join(c.Hosts, ", "), dirs.Shorten(c.KeyPath), email)
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
				if err := importCandidate(cfg, c); err != nil {
					fmt.Printf("skip %q: %v\n", c.Username, err)
					failed++
					continue
				}
				fmt.Printf("Imported %q (%s)\n", c.Username, dirs.Shorten(c.KeyPath))
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

// importCandidate creates the SSH profile and — when the email is
// known — the Git profile with the candidate's directories.
func importCandidate(cfg *config.Config, c importer.Candidate) error {
	if _, err := sshprofile.Add(cfg, config.SSHProfile{
		Username:  c.Username,
		KeyPath:   c.KeyPath,
		HostAlias: c.HostAlias,
		Providers: c.Hosts,
	}, sshprofile.AddOpts{}); err != nil {
		return fmt.Errorf("ssh profile: %w", err)
	}
	if c.Email == "" {
		fmt.Printf("  (no email known: only the ssh side of %q was imported; add it later with gpm profile add)\n", c.Username)
		return nil
	}
	directory := ""
	if len(c.Directories) > 0 {
		directory = c.Directories[0]
	}
	if _, err := gitprofile.Add(cfg, config.GitProfile{
		Username: c.Username,
		Email:    c.Email,
	}, directory); err != nil {
		return fmt.Errorf("git profile: %w", err)
	}
	for _, d := range c.Directories[1:] {
		if _, err := dirs.Add(cfg, d, c.Username, false); err != nil {
			return fmt.Errorf("directory %s: %w", dirs.Shorten(d), err)
		}
	}
	return nil
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
		options = append(options, huh.Option[int]{
			Key:   fmt.Sprintf("%s (%s)", c.Username, c.Email),
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
