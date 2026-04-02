package cli

import (
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"

	"github.com/plombardi89/codebox/internal/datadir"
	"github.com/plombardi89/codebox/internal/provider"
	azureprovider "github.com/plombardi89/codebox/internal/provider/azure"
	"github.com/plombardi89/codebox/internal/state"
)

func newDownCmd(reg *provider.Registry, dataDir *string, log *slog.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "down <name>",
		Short: "Stop a codebox",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDown(cmd, args, reg, *dataDir, log)
		},
	}

	cmd.Flags().Bool("delete", false, "delete the remote resources after stopping")
	cmd.Flags().Bool("delete-local-storage", false, "remove local box directory after stopping")
	cmd.Flags().String("azure-subscription-id", "", "Azure subscription ID (overrides AZURE_SUBSCRIPTION_ID)")

	return cmd
}

func runDown(cmd *cobra.Command, args []string, reg *provider.Registry, dataDir string, log *slog.Logger) error {
	name := args[0]
	boxDir := datadir.BoxDir(dataDir, name)
	stateFile := state.Path(boxDir)

	st, err := state.Load(stateFile)
	if err != nil {
		return fmt.Errorf("loading state: %w", err)
	}

	p, err := reg.Get(st.Provider)
	if err != nil {
		return fmt.Errorf("getting provider: %w", err)
	}

	// If the caller supplied --azure-subscription-id, inject it into
	// ProviderData so the Azure provider's resolveSubscriptionID picks it up
	// (Down/Delete don't accept an opts map).
	azSubID, err := getFlag(cmd, "azure-subscription-id")
	if err != nil {
		return err
	}

	if azSubID != "" {
		st.EnsureProviderData()
		st.ProviderData[azureprovider.KeySubscriptionID] = azSubID
	}

	log.Info("stopping box", "name", name)

	st, err = p.Down(cmd.Context(), st)
	if err != nil {
		return fmt.Errorf("stopping box: %w", err)
	}

	deleteRemote, err := getBoolFlag(cmd, "delete")
	if err != nil {
		return err
	}

	deleteLocal, err := getBoolFlag(cmd, "delete-local-storage")
	if err != nil {
		return err
	}

	if deleteRemote {
		log.Info("deleting remote resources", "name", name)

		if err := p.Delete(cmd.Context(), st); err != nil {
			return fmt.Errorf("deleting remote resources: %w", err)
		}
	}

	if deleteLocal {
		log.Info("removing local storage", "name", name)

		if err := datadir.RemoveBoxDir(dataDir, name); err != nil {
			return fmt.Errorf("removing local storage: %w", err)
		}
	} else {
		// Only save state if we're not deleting local storage.
		if err := state.Save(stateFile, st, log); err != nil {
			return fmt.Errorf("saving state: %w", err)
		}
	}

	fmt.Printf("codebox %s stopped\n", name)

	return nil
}
