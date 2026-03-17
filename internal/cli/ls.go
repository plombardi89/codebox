package cli

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/plombardi89/codebox/internal/state"
	"github.com/spf13/cobra"
)

func init() {
	lsCmd := &cobra.Command{
		Use:   "ls",
		Short: "List all codeboxes",
		Args:  cobra.NoArgs,
		RunE:  runLs,
	}

	rootCmd.AddCommand(lsCmd)
}

func runLs(cmd *cobra.Command, args []string) error {
	boxes, err := state.ListAll(DataDir)
	if err != nil {
		return fmt.Errorf("listing boxes: %w", err)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(w, "NAME\tSTATUS\tPROVIDER\tIMAGE\tIP\tSSH PORT"); err != nil {
		return fmt.Errorf("writing header: %w", err)
	}

	for _, b := range boxes {
		if _, err := fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%d\n", b.Name, b.Status, b.Provider, b.Image, b.IP, b.SSHPort); err != nil {
			return fmt.Errorf("writing row: %w", err)
		}
	}

	return w.Flush()
}
