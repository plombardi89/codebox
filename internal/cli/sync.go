package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v6"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v6"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
	"github.com/Azure/azure-sdk-for-go/sdk/security/keyvault/azsecrets"
	"github.com/spf13/cobra"

	"github.com/plombardi89/codebox/internal/azureutil"
	"github.com/plombardi89/codebox/internal/datadir"
	azureprovider "github.com/plombardi89/codebox/internal/provider/azure"
	"github.com/plombardi89/codebox/internal/sshkey"
	"github.com/plombardi89/codebox/internal/state"
)

func newSyncCmd(dataDir *string, log *slog.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Sync Azure codeboxes to this machine",
		Long:  "Discovers all Azure codeboxes belonging to the current user and syncs SSH keys and state locally.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSync(cmd, *dataDir, log)
		},
	}

	cmd.Flags().String("azure-subscription-id", "", "Azure subscription ID (overrides AZURE_SUBSCRIPTION_ID)")

	return cmd
}

func runSync(cmd *cobra.Command, dataDir string, log *slog.Logger) error {
	// Resolve subscription ID: flag > env var.
	subID, err := getFlag(cmd, "azure-subscription-id")
	if err != nil {
		return err
	}

	if subID == "" {
		subID = os.Getenv("AZURE_SUBSCRIPTION_ID")
	}

	if subID == "" {
		return fmt.Errorf("azure subscription ID is required: use --azure-subscription-id or set AZURE_SUBSCRIPTION_ID")
	}

	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return fmt.Errorf("creating Azure credential: %w", err)
	}

	identity, err := azureutil.IdentityFromCredential(cmd.Context(), cred)
	if err != nil {
		return fmt.Errorf("resolving Azure identity: %w", err)
	}

	log.Info("resolved user identity", "object_id", identity.ObjectID)

	// List all resource groups and filter by codebox tags.
	rgClient, err := armresources.NewResourceGroupsClient(subID, cred, nil)
	if err != nil {
		return fmt.Errorf("creating resource groups client: %w", err)
	}

	fmt.Println("Syncing Azure codeboxes...")

	var syncedCount, skippedCount int

	pager := rgClient.NewListPager(nil)
	for pager.More() {
		page, err := pager.NextPage(cmd.Context())
		if err != nil {
			return fmt.Errorf("listing resource groups: %w", err)
		}

		for _, rg := range page.Value {
			if rg.Name == nil || rg.Tags == nil {
				continue
			}

			// Check tags to identify codebox-managed resource groups for this user.
			managed := azureutil.TagValue(rg.Tags, "codebox-managed")

			userID := azureutil.TagValue(rg.Tags, "codebox-user-id")
			if managed != "true" || userID != identity.ObjectID {
				continue
			}

			name := strings.TrimPrefix(*rg.Name, "codebox-")
			if name == *rg.Name {
				// Resource group name doesn't follow our naming convention.
				log.Debug("skipping non-codebox resource group", "name", *rg.Name)
				continue
			}

			boxDir := datadir.BoxDir(dataDir, name)
			stateFile := state.Path(boxDir)

			// If local state already exists, skip.
			if _, err := os.Stat(stateFile); err == nil {
				fmt.Printf("  %s: already local, skipped\n", name)

				skippedCount++

				continue
			}

			log.Info("syncing codebox", "name", name, "resource_group", *rg.Name)

			st, err := syncBox(cmd.Context(), subID, cred, dataDir, name, *rg.Name, rg.Tags, log)
			if err != nil {
				fmt.Fprintf(os.Stderr, "  %s: error: %v\n", name, err)
				continue
			}

			if err := state.Save(stateFile, st, log); err != nil {
				fmt.Fprintf(os.Stderr, "  %s: error saving state: %v\n", name, err)
				continue
			}

			statusLine := st.Status
			if st.IP != "" {
				statusLine += ", " + st.IP
			}

			fmt.Printf("  %s: synced (%s)\n", name, statusLine)

			syncedCount++
		}
	}

	fmt.Printf("Done. Synced %d new box(es), %d already local.\n", syncedCount, skippedCount)

	return nil
}

// syncBox performs the full sync for a single Azure codebox: downloads the SSH
// key from Key Vault, queries VM state, and constructs a Box.
func syncBox(
	ctx context.Context,
	subID string,
	cred *azidentity.DefaultAzureCredential,
	dataDir, name, rgName string,
	tags map[string]*string,
	log *slog.Logger,
) (*state.Box, error) {
	// 1. Ensure local directory structure exists.
	if err := datadir.EnsureBoxDir(dataDir, name); err != nil {
		return nil, fmt.Errorf("creating box directory: %w", err)
	}

	// 2. Retrieve SSH private key from Key Vault.
	rgID := azureutil.ResourceGroupID(subID, rgName)
	kvName := azureutil.KeyVaultName(rgID)
	kvURL := azureutil.KeyVaultURL(kvName)

	log.Info("retrieving SSH key from Key Vault", "vault", kvName)

	secretClient, err := azsecrets.NewClient(kvURL, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("creating Key Vault secrets client: %w", err)
	}

	resp, err := secretClient.GetSecret(ctx, azureutil.SSHPrivateKeySecretName, "", nil)
	if err != nil {
		return nil, fmt.Errorf("retrieving SSH private key from Key Vault: %w", err)
	}

	if resp.Value == nil {
		return nil, fmt.Errorf("SSH private key secret has no value")
	}

	privKeyPEM := []byte(*resp.Value)

	pubKey, err := sshkey.DerivePublicKey(privKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("deriving public key: %w", err)
	}

	sshDir := datadir.SSHDir(dataDir, name)
	if err := sshkey.WriteKeyPair(sshDir, privKeyPEM, pubKey); err != nil {
		return nil, fmt.Errorf("writing SSH keys: %w", err)
	}

	// 3. Query VM to get status, IP, and image.
	vmClient, err := armcompute.NewVirtualMachinesClient(subID, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("creating VM client: %w", err)
	}

	vm, err := vmClient.Get(ctx, rgName, "codebox", &armcompute.VirtualMachinesClientGetOptions{
		Expand: to.Ptr(armcompute.InstanceViewTypesInstanceView),
	})
	if err != nil {
		return nil, fmt.Errorf("getting VM: %w", err)
	}

	// Determine power state.
	status := state.StatusUnknown

	if vm.Properties != nil && vm.Properties.InstanceView != nil {
		powerState := azureutil.ExtractPowerState(vm.Properties.InstanceView.Statuses)
		switch powerState {
		case "running":
			status = state.StatusUp
		case "deallocated":
			status = state.StatusDown
		}
	}

	// Determine image name.
	image := ""

	if vm.Properties != nil && vm.Properties.StorageProfile != nil && vm.Properties.StorageProfile.ImageReference != nil {
		ref := vm.Properties.StorageProfile.ImageReference
		if ref.CommunityGalleryImageID != nil {
			// Extract image name from community gallery path.
			parts := strings.Split(*ref.CommunityGalleryImageID, "/")
			for i, p := range parts {
				if p == "Images" && i+1 < len(parts) {
					image = parts[i+1]
					break
				}
			}
		}

		if image == "" && ref.Offer != nil {
			image = *ref.Offer
		}
	}

	// Fetch public IP if the VM is up.
	var ip string

	if status == state.StatusUp {
		pipClient, err := armnetwork.NewPublicIPAddressesClient(subID, cred, nil)
		if err != nil {
			return nil, fmt.Errorf("creating public IP client: %w", err)
		}

		pipResp, err := pipClient.Get(ctx, rgName, "codebox", nil)
		if err != nil {
			log.Debug("could not fetch public IP", "error", err)
		} else if pipResp.Properties != nil && pipResp.Properties.IPAddress != nil {
			ip = *pipResp.Properties.IPAddress
		}
	}

	// 4. Read SSH port from resource group tag.
	sshPort := state.DefaultSSHPort
	if portStr := azureutil.TagValue(tags, "codebox-ssh-port"); portStr != "" {
		if _, err := fmt.Sscanf(portStr, "%d", &sshPort); err != nil {
			log.Warn("could not parse SSH port tag, using default", "port_str", portStr, "error", err)
		}
	}

	now := time.Now()

	return &state.Box{
		Name:     name,
		Provider: "azure",
		Status:   status,
		IP:       ip,
		SSHPort:  sshPort,
		Image:    image,
		ProviderData: map[string]string{
			azureprovider.KeyResourceGroup:  rgName,
			azureprovider.KeyVMName:         "codebox",
			azureprovider.KeyKeyVaultName:   kvName,
			azureprovider.KeySubscriptionID: subID,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}
