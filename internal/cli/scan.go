package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/tonmoydeb404/gpm/internal/dirs"
	"github.com/tonmoydeb404/gpm/internal/importer"
)

func newScanCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "scan",
		Short: "Report an existing Git/SSH setup without changing anything",
		Long: `Inspect ~/.ssh/config (including Include'd files, resolved via
ssh -G) and the global gitconfig's include/includeIf chain.

Scan is config-only and read-only. Use gpm import to act on what it finds.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			rep := importer.Scan(cfg, importer.ScanOptions{})
			printReport(rep)
			return nil
		},
	}
	return cmd
}

// printReport renders a scan report as plain text.
func printReport(rep *importer.Report) {
	for _, w := range rep.Warnings {
		fmt.Println("note:", w)
	}

	if len(rep.Candidates) == 0 {
		fmt.Println("No existing multi-account setup detected.")
	} else {
		fmt.Printf("\nProposed accounts (%d):\n", len(rep.Candidates))
		for _, c := range rep.Candidates {
			email := c.Email
			if email == "" {
				email = "(email unknown)"
			}
			alias := c.HostAlias
			if alias == "" {
				alias = "(global)"
			}
			fmt.Printf("  %-16s %s [%s] -> %s (%s)\n", c.Username, strings.Join(c.Hosts, ", "), alias, dirs.Shorten(c.KeyPath), email)
			if c.Fingerprint != "" {
				fmt.Printf("  %-16s fingerprint: %s\n", "", c.Fingerprint)
			}
			for _, d := range c.Directories {
				fmt.Printf("  %-16s directory: %s\n", "", dirs.Shorten(d))
			}
		}
	}

	fmt.Printf("\nScanned in %s.\n", rep.Duration.Round(10*time.Millisecond))
	fmt.Println("\nScan only; nothing was written. Re-run gpm import to migrate.")
}
