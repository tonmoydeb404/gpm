package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/tonmoydeb404/gpm/internal/doctor"
)

func newDoctorCmd() *cobra.Command {
	var fix, network bool
	var dir string
	cmd := &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose the gpm setup and report actionable problems",
		Long: `Run health checks over profiles, keys and permissions, the managed
SSH config block, gitconfig includeIf rules, directory mappings, and
the identity of the current repository.

Use --network to also test live GitHub authentication per profile and
--fix to auto-repair what can be repaired (permissions, missing
managed blocks, stale rules).`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			if dir == "" {
				dir, err = os.Getwd()
				if err != nil {
					return err
				}
			}
			results := doctor.Run(cfg, doctor.Options{Fix: fix, Network: network, Dir: dir})

			counts := map[doctor.Status]int{}
			for _, r := range results {
				counts[r.Status]++
				icon := "✅"
				switch r.Status {
				case doctor.Warn:
					icon = "⚠️ "
				case doctor.Fail:
					icon = "❌"
				}
				fmt.Printf("%s %-22s %s\n", icon, r.Name, r.Detail)
				if r.Hint != "" {
					fmt.Printf("   %s\n", r.Hint)
				}
			}
			fmt.Printf("\n%d passed, %d warnings, %d failures\n", counts[doctor.Pass], counts[doctor.Warn], counts[doctor.Fail])
			if counts[doctor.Fail] > 0 {
				os.Exit(1)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&fix, "fix", false, "auto-repair fixable problems (permissions, managed blocks)")
	cmd.Flags().BoolVar(&network, "network", false, "also test live GitHub SSH authentication")
	cmd.Flags().StringVar(&dir, "dir", "", "directory for the repo identity check (default: working directory)")
	return cmd
}
