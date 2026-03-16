package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/voidfunktion/ocbox/internal/datadir"
	"github.com/voidfunktion/ocbox/internal/provider"
	"github.com/voidfunktion/ocbox/internal/state"
)

func init() {
	downCmd := &cobra.Command{
		Use:   "down <name>",
		Short: "Stop a codebox",
		Args:  cobra.ExactArgs(1),
		RunE:  runDown,
	}

	downCmd.Flags().Bool("delete", false, "delete the remote resources after stopping")
	downCmd.Flags().Bool("delete-local-storage", false, "remove local box directory after stopping")

	rootCmd.AddCommand(downCmd)
}

func runDown(cmd *cobra.Command, args []string) error {
	name := args[0]
	boxDir := datadir.BoxDir(DataDir, name)
	stateFile := state.StatePath(boxDir)

	st, err := state.Load(stateFile)
	if err != nil {
		return fmt.Errorf("loading state: %w", err)
	}

	p, err := provider.Get(st.Provider)
	if err != nil {
		return fmt.Errorf("getting provider: %w", err)
	}

	st, err = p.Down(cmd.Context(), st)
	if err != nil {
		return fmt.Errorf("stopping box: %w", err)
	}

	if err := state.Save(stateFile, st); err != nil {
		return fmt.Errorf("saving state: %w", err)
	}

	deleteRemote, _ := cmd.Flags().GetBool("delete")
	deleteLocal, _ := cmd.Flags().GetBool("delete-local-storage")

	if deleteRemote {
		if err := p.Delete(cmd.Context(), st); err != nil {
			return fmt.Errorf("deleting remote resources: %w", err)
		}
	}

	if deleteLocal {
		if err := datadir.RemoveBoxDir(DataDir, name); err != nil {
			return fmt.Errorf("removing local storage: %w", err)
		}
	}

	if !deleteRemote && !deleteLocal {
		if err := state.Save(stateFile, st); err != nil {
			return fmt.Errorf("saving state: %w", err)
		}
	}

	fmt.Printf("codebox %s stopped\n", name)
	return nil
}
