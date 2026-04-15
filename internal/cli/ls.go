package cli

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/plombardi89/codebox/internal/state"
)

func newLsCmd(dataDir *string) *cobra.Command {
	return &cobra.Command{
		Use:   "ls",
		Short: "List all codeboxes",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLs(*dataDir)
		},
	}
}

func runLs(dataDir string) error {
	boxes, err := state.ListAll(dataDir)
	if err != nil {
		return fmt.Errorf("listing boxes: %w", err)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(w, "NAME\tSTATUS\tPROVIDER\tIMAGE\tPROFILE\tIP\tSSH PORT"); err != nil {
		return fmt.Errorf("writing header: %w", err)
	}

	for _, b := range boxes {
		prof := b.Profile
		if prof == "" {
			prof = "-"
		}

		if _, err := fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%d\n", b.Name, b.Status, b.Provider, b.Image, prof, b.IP, b.SSHPort); err != nil {
			return fmt.Errorf("writing row: %w", err)
		}
	}

	return w.Flush()
}
