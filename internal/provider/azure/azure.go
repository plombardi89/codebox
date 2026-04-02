package azure

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v6"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/keyvault/armkeyvault"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v6"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"
	"github.com/Azure/azure-sdk-for-go/sdk/security/keyvault/azsecrets"

	"github.com/plombardi89/codebox/internal/azureutil"
	"github.com/plombardi89/codebox/internal/provider"
	"github.com/plombardi89/codebox/internal/state"
)

// isNotFound reports whether err is an Azure API 404 Not Found response.
func isNotFound(err error) bool {
	var respErr *azcore.ResponseError

	return errors.As(err, &respErr) && respErr.StatusCode == http.StatusNotFound
}

// errVMNotFound is returned by resumeExistingVM when the VM recorded in state
// no longer exists in Azure (e.g. manually deleted). The caller falls through
// to the full create path.
var errVMNotFound = errors.New("VM not found")

// Provider data keys persisted in Box.ProviderData.
const (
	KeyResourceGroup  = "resource_group"
	KeyVMName         = "vm_name"
	KeyKeyVaultName   = "key_vault_name"
	KeySubscriptionID = "subscription_id"
)

// New returns a new Azure provider that logs to log.
func New(log *slog.Logger) provider.Provider {
	return &azureProvider{log: log}
}

// azureProvider implements provider.Provider using the Azure SDK.
type azureProvider struct {
	log *slog.Logger
}

// resolveSubscriptionID returns the Azure subscription ID by checking the
// opts map first, then the ProviderData on the box state, and finally the
// AZURE_SUBSCRIPTION_ID environment variable.
func resolveSubscriptionID(opts map[string]string, st *state.Box) (string, error) {
	// 1. Explicit opts (from CLI flag).
	if id := opts["subscription-id"]; id != "" {
		return id, nil
	}
	// 2. Persisted in state from a previous Up().
	if st != nil && st.ProviderData != nil {
		if id := st.ProviderData[KeySubscriptionID]; id != "" {
			return id, nil
		}
	}
	// 3. Environment variable.
	if id := os.Getenv("AZURE_SUBSCRIPTION_ID"); id != "" {
		return id, nil
	}

	return "", fmt.Errorf("azure subscription ID is required: use --azure-subscription-id or set AZURE_SUBSCRIPTION_ID")
}

// azureAuth resolves the subscription ID and creates an Azure credential.
// It is the common preamble for every provider method.
func azureAuth(opts map[string]string, st *state.Box) (string, *azidentity.DefaultAzureCredential, error) {
	subID, err := resolveSubscriptionID(opts, st)
	if err != nil {
		return "", nil, err
	}

	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return "", nil, fmt.Errorf("creating Azure credential: %w", err)
	}

	return subID, cred, nil
}

func (a *azureProvider) Up(ctx context.Context, st *state.Box, pubKey string, opts map[string]string) (*state.Box, error) {
	subID, cred, err := azureAuth(opts, st)
	if err != nil {
		return nil, err
	}

	vmSize := provider.OptOrDefault(opts, "vm-size", "standard_d2ads_v6")
	location := provider.OptOrDefault(opts, "location", "canadacentral")
	rgName := fmt.Sprintf("codebox-%s", st.Name)

	// Idempotency: if we already have a VM, check its state.
	if rg, ok := st.ProviderData[KeyResourceGroup]; ok {
		if vmName, ok2 := st.ProviderData[KeyVMName]; ok2 {
			resumed, err := a.resumeExistingVM(ctx, subID, cred, st, rg, vmName)
			if err == nil {
				return resumed, nil
			}

			if !errors.Is(err, errVMNotFound) {
				return nil, err
			}

			// VM was deleted externally; fall through to recreate it.
			// All Azure create calls use BeginCreateOrUpdate, so existing
			// resources (RG, VNet, NSG, PIP, NIC, Key Vault) are no-ops.
			a.log.Info("recreating VM within existing resource group", "resource_group", rg)
		}
	}

	// No existing VM -- create from scratch.
	st.EnsureProviderData()

	a.log.Info("creating resource group", "name", rgName, "location", location)

	identity, nsgResp, err := a.createNetworkPrereqs(ctx, subID, cred, rgName, location)
	if err != nil {
		return nil, err
	}

	subnetID, err := a.createVNet(ctx, subID, cred, rgName, location)
	if err != nil {
		return nil, err
	}

	pipResp, err := a.createPublicIP(ctx, subID, cred, rgName, location)
	if err != nil {
		return nil, err
	}

	nicResp, err := a.createNIC(ctx, subID, cred, rgName, location, nsgResp.ID, subnetID, pipResp.ID)
	if err != nil {
		return nil, err
	}

	userData := base64.StdEncoding.EncodeToString([]byte(provider.OptOrDefault(opts, "user-data", "")))
	if err := a.createVM(ctx, subID, cred, rgName, location, vmSize, pubKey, userData, nicResp.ID); err != nil {
		return nil, err
	}

	kvName, err := a.createKeyVaultAndStoreKey(ctx, subID, cred, identity, rgName, location, opts["ssh-private-key"])
	if err != nil {
		return nil, err
	}

	// Re-fetch public IP to get assigned address.
	ip, err := fetchPublicIP(ctx, subID, cred, rgName)
	if err != nil {
		return nil, fmt.Errorf("fetching public IP: %w", err)
	}

	st.SetUp(ip)
	st.Image = "Fedora-Cloud-43-x64"
	st.ProviderData[KeyResourceGroup] = rgName
	st.ProviderData[KeyVMName] = "codebox"
	st.ProviderData[KeyKeyVaultName] = kvName
	st.ProviderData[KeySubscriptionID] = subID
	a.log.Debug("VM created", "resource_group", rgName, "ip", st.IP)

	return st, nil
}

// resumeExistingVM handles the idempotent Up() path when a VM already exists.
func (a *azureProvider) resumeExistingVM(ctx context.Context, subID string, cred azcore.TokenCredential, st *state.Box, rg, vmName string) (*state.Box, error) {
	a.log.Debug("existing VM found in state", "resource_group", rg, "vm_name", vmName)

	vmClient, err := armcompute.NewVirtualMachinesClient(subID, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("creating VM client: %w", err)
	}

	resp, err := vmClient.InstanceView(ctx, rg, vmName, nil)
	if err != nil {
		if isNotFound(err) {
			a.log.Info("VM recorded in state no longer exists, will recreate", "resource_group", rg, "vm_name", vmName)
			return nil, errVMNotFound
		}

		return nil, fmt.Errorf("getting VM instance view: %w", err)
	}

	powerState := azureutil.ExtractPowerState(resp.Statuses)
	switch powerState {
	case "running":
		a.log.Debug("VM already running", "name", st.Name)

		ip, err := fetchPublicIP(ctx, subID, cred, rg)
		if err != nil {
			return nil, fmt.Errorf("fetching public IP: %w", err)
		}

		st.SetUp(ip)

		return st, nil

	case "deallocated":
		a.log.Info("starting deallocated VM", "name", st.Name)

		poller, err := vmClient.BeginStart(ctx, rg, vmName, nil)
		if err != nil {
			return nil, fmt.Errorf("starting VM: %w", err)
		}

		if _, err := poller.PollUntilDone(ctx, nil); err != nil {
			return nil, fmt.Errorf("waiting for VM start: %w", err)
		}

		ip, err := fetchPublicIP(ctx, subID, cred, rg)
		if err != nil {
			return nil, fmt.Errorf("fetching public IP: %w", err)
		}

		st.SetUp(ip)

		return st, nil

	default:
		return nil, fmt.Errorf("VM is in unexpected power state: %s", powerState)
	}
}

// createNetworkPrereqs creates the resource group, resolves the user identity,
// and creates the NSG. It returns the identity info and NSG response.
func (a *azureProvider) createNetworkPrereqs(
	ctx context.Context, subID string, cred azcore.TokenCredential,
	rgName, location string,
) (*azureutil.IdentityInfo, armnetwork.SecurityGroupsClientCreateOrUpdateResponse, error) {
	var nsgResp armnetwork.SecurityGroupsClientCreateOrUpdateResponse

	rgClient, err := armresources.NewResourceGroupsClient(subID, cred, nil)
	if err != nil {
		return nil, nsgResp, fmt.Errorf("creating resource groups client: %w", err)
	}

	identity, err := azureutil.IdentityFromCredential(ctx, cred)
	if err != nil {
		return nil, nsgResp, fmt.Errorf("resolving Azure identity: %w", err)
	}

	_, err = rgClient.CreateOrUpdate(ctx, rgName, armresources.ResourceGroup{
		Location: to.Ptr(location),
		Tags: map[string]*string{
			"codebox-managed":  to.Ptr("true"),
			"codebox-user-id":  to.Ptr(identity.ObjectID),
			"codebox-ssh-port": to.Ptr(fmt.Sprintf("%d", state.DefaultSSHPort)),
		},
	}, nil)
	if err != nil {
		return nil, nsgResp, fmt.Errorf("creating resource group: %w", err)
	}

	// Create NSG with SSH rule. For Microsoft users, restrict inbound SSH to
	// the CorpNetPublic service tag.
	sourcePrefix := "*"
	if azureutil.IsMicrosoftUser(identity) {
		sourcePrefix = "CorpNetPublic"

		a.log.Info("using CorpNetPublic service tag for NSG rule")
	}

	a.log.Info("creating NSG", "name", "codebox", "resource_group", rgName)

	nsgClient, err := armnetwork.NewSecurityGroupsClient(subID, cred, nil)
	if err != nil {
		return nil, nsgResp, fmt.Errorf("creating NSG client: %w", err)
	}

	nsgPoller, err := nsgClient.BeginCreateOrUpdate(ctx, rgName, "codebox", armnetwork.SecurityGroup{
		Location: to.Ptr(location),
		Properties: &armnetwork.SecurityGroupPropertiesFormat{
			SecurityRules: []*armnetwork.SecurityRule{
				{
					Name: to.Ptr("allow-ssh"),
					Properties: &armnetwork.SecurityRulePropertiesFormat{
						Priority:                 to.Ptr(int32(100)),
						Protocol:                 to.Ptr(armnetwork.SecurityRuleProtocolTCP),
						Access:                   to.Ptr(armnetwork.SecurityRuleAccessAllow),
						Direction:                to.Ptr(armnetwork.SecurityRuleDirectionInbound),
						SourceAddressPrefix:      to.Ptr(sourcePrefix),
						SourcePortRange:          to.Ptr("*"),
						DestinationAddressPrefix: to.Ptr("*"),
						DestinationPortRange:     to.Ptr(fmt.Sprintf("%d", state.DefaultSSHPort)),
					},
				},
			},
		},
	}, nil)
	if err != nil {
		return nil, nsgResp, fmt.Errorf("creating NSG: %w", err)
	}

	nsgResp, err = nsgPoller.PollUntilDone(ctx, nil)
	if err != nil {
		return nil, nsgResp, fmt.Errorf("waiting for NSG creation: %w", err)
	}

	return identity, nsgResp, nil
}

// createVNet creates a virtual network with a default subnet and returns the subnet ID.
func (a *azureProvider) createVNet(ctx context.Context, subID string, cred azcore.TokenCredential, rgName, location string) (string, error) {
	a.log.Info("creating VNet", "name", "codebox", "resource_group", rgName)

	vnetClient, err := armnetwork.NewVirtualNetworksClient(subID, cred, nil)
	if err != nil {
		return "", fmt.Errorf("creating VNet client: %w", err)
	}

	vnetPoller, err := vnetClient.BeginCreateOrUpdate(ctx, rgName, "codebox", armnetwork.VirtualNetwork{
		Location: to.Ptr(location),
		Properties: &armnetwork.VirtualNetworkPropertiesFormat{
			AddressSpace: &armnetwork.AddressSpace{
				AddressPrefixes: []*string{to.Ptr("10.0.0.0/16")},
			},
			Subnets: []*armnetwork.Subnet{
				{
					Name: to.Ptr("default"),
					Properties: &armnetwork.SubnetPropertiesFormat{
						AddressPrefix: to.Ptr("10.0.0.0/24"),
					},
				},
			},
		},
	}, nil)
	if err != nil {
		return "", fmt.Errorf("creating VNet: %w", err)
	}

	vnetResp, err := vnetPoller.PollUntilDone(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("waiting for VNet creation: %w", err)
	}

	if vnetResp.Properties != nil && len(vnetResp.Properties.Subnets) > 0 {
		return *vnetResp.Properties.Subnets[0].ID, nil
	}

	return "", fmt.Errorf("subnet not found in VNet response")
}

// createPublicIP creates a static public IP address.
func (a *azureProvider) createPublicIP(ctx context.Context, subID string, cred azcore.TokenCredential, rgName, location string) (armnetwork.PublicIPAddressesClientCreateOrUpdateResponse, error) {
	a.log.Info("creating public IP", "name", "codebox", "resource_group", rgName)

	pipClient, err := armnetwork.NewPublicIPAddressesClient(subID, cred, nil)
	if err != nil {
		return armnetwork.PublicIPAddressesClientCreateOrUpdateResponse{}, fmt.Errorf("creating public IP client: %w", err)
	}

	pipPoller, err := pipClient.BeginCreateOrUpdate(ctx, rgName, "codebox", armnetwork.PublicIPAddress{
		Location: to.Ptr(location),
		SKU: &armnetwork.PublicIPAddressSKU{
			Name: to.Ptr(armnetwork.PublicIPAddressSKUNameStandard),
		},
		Properties: &armnetwork.PublicIPAddressPropertiesFormat{
			PublicIPAllocationMethod: to.Ptr(armnetwork.IPAllocationMethodStatic),
		},
	}, nil)
	if err != nil {
		return armnetwork.PublicIPAddressesClientCreateOrUpdateResponse{}, fmt.Errorf("creating public IP: %w", err)
	}

	resp, err := pipPoller.PollUntilDone(ctx, nil)
	if err != nil {
		return armnetwork.PublicIPAddressesClientCreateOrUpdateResponse{}, fmt.Errorf("waiting for public IP creation: %w", err)
	}

	return resp, nil
}

// createNIC creates a network interface attached to the given subnet, public IP, and NSG.
func (a *azureProvider) createNIC(
	ctx context.Context, subID string, cred azcore.TokenCredential,
	rgName, location string, nsgID *string, subnetID string, pipID *string,
) (armnetwork.InterfacesClientCreateOrUpdateResponse, error) {
	a.log.Info("creating NIC", "name", "codebox", "resource_group", rgName)

	nicClient, err := armnetwork.NewInterfacesClient(subID, cred, nil)
	if err != nil {
		return armnetwork.InterfacesClientCreateOrUpdateResponse{}, fmt.Errorf("creating NIC client: %w", err)
	}

	nicPoller, err := nicClient.BeginCreateOrUpdate(ctx, rgName, "codebox", armnetwork.Interface{
		Location: to.Ptr(location),
		Properties: &armnetwork.InterfacePropertiesFormat{
			NetworkSecurityGroup: &armnetwork.SecurityGroup{ID: nsgID},
			IPConfigurations: []*armnetwork.InterfaceIPConfiguration{
				{
					Name: to.Ptr("ipconfig1"),
					Properties: &armnetwork.InterfaceIPConfigurationPropertiesFormat{
						Subnet:                    &armnetwork.Subnet{ID: to.Ptr(subnetID)},
						PublicIPAddress:           &armnetwork.PublicIPAddress{ID: pipID},
						PrivateIPAllocationMethod: to.Ptr(armnetwork.IPAllocationMethodDynamic),
					},
				},
			},
		},
	}, nil)
	if err != nil {
		return armnetwork.InterfacesClientCreateOrUpdateResponse{}, fmt.Errorf("creating NIC: %w", err)
	}

	resp, err := nicPoller.PollUntilDone(ctx, nil)
	if err != nil {
		return armnetwork.InterfacesClientCreateOrUpdateResponse{}, fmt.Errorf("waiting for NIC creation: %w", err)
	}

	return resp, nil
}

// createVM creates the virtual machine with the given configuration.
func (a *azureProvider) createVM(
	ctx context.Context, subID string, cred azcore.TokenCredential,
	rgName, location, vmSize, pubKey, userData string, nicID *string,
) error {
	a.log.Info("creating VM", "name", "codebox", "resource_group", rgName, "vm_size", vmSize)

	vmClient, err := armcompute.NewVirtualMachinesClient(subID, cred, nil)
	if err != nil {
		return fmt.Errorf("creating VM client: %w", err)
	}

	vmPoller, err := vmClient.BeginCreateOrUpdate(ctx, rgName, "codebox", armcompute.VirtualMachine{
		Location: to.Ptr(location),
		Properties: &armcompute.VirtualMachineProperties{
			HardwareProfile: &armcompute.HardwareProfile{
				VMSize: to.Ptr(armcompute.VirtualMachineSizeTypes(vmSize)),
			},
			DiagnosticsProfile: &armcompute.DiagnosticsProfile{
				BootDiagnostics: &armcompute.BootDiagnostics{
					Enabled: to.Ptr(true),
				},
			},
			StorageProfile: &armcompute.StorageProfile{
				ImageReference: &armcompute.ImageReference{
					CommunityGalleryImageID: to.Ptr("/CommunityGalleries/Fedora-5e266ba4-2250-406d-adad-5d73860d958f/Images/Fedora-Cloud-43-x64/Versions/latest"),
				},
				OSDisk: &armcompute.OSDisk{
					CreateOption: to.Ptr(armcompute.DiskCreateOptionTypesFromImage),
					ManagedDisk: &armcompute.ManagedDiskParameters{
						StorageAccountType: to.Ptr(armcompute.StorageAccountTypesStandardSSDLRS),
					},
				},
			},
			OSProfile: &armcompute.OSProfile{
				ComputerName:  to.Ptr("codebox"),
				AdminUsername: to.Ptr(state.DefaultUser),
				LinuxConfiguration: &armcompute.LinuxConfiguration{
					DisablePasswordAuthentication: to.Ptr(true),
					SSH: &armcompute.SSHConfiguration{
						PublicKeys: []*armcompute.SSHPublicKey{
							{
								Path:    to.Ptr(fmt.Sprintf("/home/%s/.ssh/authorized_keys", state.DefaultUser)),
								KeyData: to.Ptr(pubKey),
							},
						},
					},
				},
			},
			NetworkProfile: &armcompute.NetworkProfile{
				NetworkInterfaces: []*armcompute.NetworkInterfaceReference{
					{
						ID: nicID,
						Properties: &armcompute.NetworkInterfaceReferenceProperties{
							Primary: to.Ptr(true),
						},
					},
				},
			},
			UserData: to.Ptr(userData),
		},
	}, nil)
	if err != nil {
		return fmt.Errorf("creating VM: %w", err)
	}

	if _, err := vmPoller.PollUntilDone(ctx, nil); err != nil {
		return fmt.Errorf("waiting for VM creation: %w", err)
	}

	return nil
}

// createKeyVaultAndStoreKey creates a Key Vault and stores the SSH private key
// as a secret. Returns the Key Vault name.
func (a *azureProvider) createKeyVaultAndStoreKey(
	ctx context.Context, subID string, cred azcore.TokenCredential,
	identity *azureutil.IdentityInfo, rgName, location, sshPrivKey string,
) (string, error) {
	rgID := azureutil.ResourceGroupID(subID, rgName)
	kvName := azureutil.KeyVaultName(rgID)
	a.log.Info("creating Key Vault", "name", kvName, "resource_group", rgName)

	if err := a.ensureKeyVaultPurged(ctx, subID, cred, kvName, location); err != nil {
		return "", fmt.Errorf("purging stale Key Vault: %w", err)
	}

	kvClient, err := armkeyvault.NewVaultsClient(subID, cred, nil)
	if err != nil {
		return "", fmt.Errorf("creating Key Vault client: %w", err)
	}

	kvPoller, err := kvClient.BeginCreateOrUpdate(ctx, rgName, kvName, armkeyvault.VaultCreateOrUpdateParameters{
		Location: to.Ptr(location),
		Properties: &armkeyvault.VaultProperties{
			TenantID: to.Ptr(identity.TenantID),
			SKU: &armkeyvault.SKU{
				Family: to.Ptr(armkeyvault.SKUFamilyA),
				Name:   to.Ptr(armkeyvault.SKUNameStandard),
			},
			AccessPolicies: []*armkeyvault.AccessPolicyEntry{
				{
					TenantID: to.Ptr(identity.TenantID),
					ObjectID: to.Ptr(identity.ObjectID),
					Permissions: &armkeyvault.Permissions{
						Secrets: []*armkeyvault.SecretPermissions{
							to.Ptr(armkeyvault.SecretPermissionsGet),
							to.Ptr(armkeyvault.SecretPermissionsSet),
							to.Ptr(armkeyvault.SecretPermissionsDelete),
						},
					},
				},
			},
		},
	}, nil)
	if err != nil {
		return "", fmt.Errorf("creating Key Vault: %w", err)
	}

	if _, err := kvPoller.PollUntilDone(ctx, nil); err != nil {
		return "", fmt.Errorf("waiting for Key Vault creation: %w", err)
	}

	if sshPrivKey != "" {
		a.log.Info("storing SSH private key in Key Vault", "vault", kvName)
		kvURL := azureutil.KeyVaultURL(kvName)

		secretClient, err := azsecrets.NewClient(kvURL, cred, nil)
		if err != nil {
			return "", fmt.Errorf("creating Key Vault secrets client: %w", err)
		}

		_, err = secretClient.SetSecret(ctx, azureutil.SSHPrivateKeySecretName, azsecrets.SetSecretParameters{
			Value: to.Ptr(sshPrivKey),
		}, nil)
		if err != nil {
			return "", fmt.Errorf("storing SSH private key in Key Vault: %w", err)
		}
	}

	return kvName, nil
}

func (a *azureProvider) Down(ctx context.Context, st *state.Box) (*state.Box, error) {
	subID, cred, err := azureAuth(nil, st)
	if err != nil {
		return nil, err
	}

	rg := st.ProviderData[KeyResourceGroup]
	vmName := st.ProviderData[KeyVMName]

	a.log.Info("deallocating VM", "resource_group", rg, "vm_name", vmName)

	vmClient, err := armcompute.NewVirtualMachinesClient(subID, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("creating VM client: %w", err)
	}

	poller, err := vmClient.BeginDeallocate(ctx, rg, vmName, nil)
	if err != nil {
		if isNotFound(err) {
			a.log.Info("VM already gone, skipping deallocation", "resource_group", rg, "vm_name", vmName)
			st.SetDown()

			return st, nil
		}

		return nil, fmt.Errorf("deallocating VM: %w", err)
	}

	if _, err := poller.PollUntilDone(ctx, nil); err != nil {
		if isNotFound(err) {
			a.log.Info("VM disappeared during deallocation", "resource_group", rg, "vm_name", vmName)
			st.SetDown()

			return st, nil
		}

		return nil, fmt.Errorf("waiting for VM deallocation: %w", err)
	}

	st.SetDown()

	return st, nil
}

func (a *azureProvider) DestroyVM(ctx context.Context, st *state.Box) error {
	subID, cred, err := azureAuth(nil, st)
	if err != nil {
		return err
	}

	rg := st.ProviderData[KeyResourceGroup]
	vmName := st.ProviderData[KeyVMName]

	vmClient, err := armcompute.NewVirtualMachinesClient(subID, cred, nil)
	if err != nil {
		return fmt.Errorf("creating VM client: %w", err)
	}

	// Fetch the VM to find the OS disk name before deleting it.
	var osDiskName string

	vmResp, err := vmClient.Get(ctx, rg, vmName, nil)
	if err != nil {
		if isNotFound(err) {
			a.log.Info("VM already gone, skipping deletion", "resource_group", rg, "vm_name", vmName)
			return nil
		}

		return fmt.Errorf("getting VM: %w", err)
	}

	if vmResp.Properties != nil && vmResp.Properties.StorageProfile != nil &&
		vmResp.Properties.StorageProfile.OSDisk != nil && vmResp.Properties.StorageProfile.OSDisk.Name != nil {
		osDiskName = *vmResp.Properties.StorageProfile.OSDisk.Name
	}

	// Delete the VM.
	a.log.Info("deleting VM", "resource_group", rg, "vm_name", vmName)

	vmPoller, err := vmClient.BeginDelete(ctx, rg, vmName, nil)
	if err != nil {
		if isNotFound(err) {
			a.log.Info("VM disappeared before deletion", "resource_group", rg, "vm_name", vmName)
			return nil
		}

		return fmt.Errorf("deleting VM: %w", err)
	}

	if _, err := vmPoller.PollUntilDone(ctx, nil); err != nil {
		return fmt.Errorf("waiting for VM deletion: %w", err)
	}

	a.log.Info("VM deleted", "resource_group", rg, "vm_name", vmName)

	// Delete the OS disk (not removed automatically by VM deletion).
	if osDiskName != "" {
		a.log.Info("deleting OS disk", "resource_group", rg, "disk_name", osDiskName)

		diskClient, err := armcompute.NewDisksClient(subID, cred, nil)
		if err != nil {
			return fmt.Errorf("creating disks client: %w", err)
		}

		diskPoller, err := diskClient.BeginDelete(ctx, rg, osDiskName, nil)
		if err != nil {
			if isNotFound(err) {
				a.log.Info("OS disk already gone", "resource_group", rg, "disk_name", osDiskName)
				return nil
			}

			return fmt.Errorf("deleting OS disk: %w", err)
		}

		if _, err := diskPoller.PollUntilDone(ctx, nil); err != nil {
			return fmt.Errorf("waiting for OS disk deletion: %w", err)
		}

		a.log.Info("OS disk deleted", "resource_group", rg, "disk_name", osDiskName)
	}

	return nil
}

func (a *azureProvider) Delete(ctx context.Context, st *state.Box) error {
	subID, cred, err := azureAuth(nil, st)
	if err != nil {
		return err
	}

	rg := st.ProviderData[KeyResourceGroup]

	a.log.Info("deleting resource group", "name", rg)

	rgClient, err := armresources.NewResourceGroupsClient(subID, cred, nil)
	if err != nil {
		return fmt.Errorf("creating resource groups client: %w", err)
	}

	poller, err := rgClient.BeginDelete(ctx, rg, nil)
	if err != nil {
		if isNotFound(err) {
			a.log.Info("resource group already gone, skipping deletion", "name", rg)
			return nil
		}

		return fmt.Errorf("deleting resource group: %w", err)
	}

	if _, err := poller.PollUntilDone(ctx, nil); err != nil {
		if isNotFound(err) {
			a.log.Info("resource group disappeared during deletion", "name", rg)
			return nil
		}

		return fmt.Errorf("waiting for resource group deletion: %w", err)
	}

	a.log.Info("resource group deleted", "name", rg)

	return nil
}

func (a *azureProvider) Status(ctx context.Context, st *state.Box) (*state.Box, error) {
	subID, cred, err := azureAuth(nil, st)
	if err != nil {
		return nil, err
	}

	rg := st.ProviderData[KeyResourceGroup]
	vmName := st.ProviderData[KeyVMName]

	vmClient, err := armcompute.NewVirtualMachinesClient(subID, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("creating VM client: %w", err)
	}

	resp, err := vmClient.InstanceView(ctx, rg, vmName, nil)
	if err != nil {
		return nil, fmt.Errorf("getting VM instance view: %w", err)
	}

	powerState := azureutil.ExtractPowerState(resp.Statuses)
	switch powerState {
	case "running":
		st.Status = state.StatusUp
	case "deallocated":
		st.Status = state.StatusDown
	default:
		st.Status = state.StatusUnknown
	}

	if st.Status == state.StatusUp {
		ip, err := fetchPublicIP(ctx, subID, cred, rg)
		if err != nil {
			return nil, fmt.Errorf("fetching public IP: %w", err)
		}

		st.IP = ip
	}

	st.UpdatedAt = time.Now()

	return st, nil
}

// fetchPublicIP retrieves the current IP address of the "codebox" public IP
// resource in the given resource group.
func fetchPublicIP(ctx context.Context, subID string, cred azcore.TokenCredential, rg string) (string, error) {
	pipClient, err := armnetwork.NewPublicIPAddressesClient(subID, cred, nil)
	if err != nil {
		return "", fmt.Errorf("creating public IP client: %w", err)
	}

	pipResp, err := pipClient.Get(ctx, rg, "codebox", nil)
	if err != nil {
		return "", fmt.Errorf("getting public IP: %w", err)
	}

	if pipResp.Properties == nil || pipResp.Properties.IPAddress == nil {
		return "", fmt.Errorf("public IP address not yet assigned")
	}

	return *pipResp.Properties.IPAddress, nil
}

// ensureKeyVaultPurged checks if there is a soft-deleted Key Vault with the
// given name and purges it so a new vault can be created with the same name.
func (a *azureProvider) ensureKeyVaultPurged(ctx context.Context, subID string, cred azcore.TokenCredential, kvName, location string) error {
	kvClient, err := armkeyvault.NewVaultsClient(subID, cred, nil)
	if err != nil {
		return fmt.Errorf("creating Key Vault client: %w", err)
	}

	// Check if a soft-deleted vault with this name exists.
	_, err = kvClient.GetDeleted(ctx, kvName, location, nil)
	if err != nil {
		if isNotFound(err) {
			return nil
		}

		// Any other error is unexpected and should be surfaced.
		return fmt.Errorf("checking for soft-deleted Key Vault: %w", err)
	}

	a.log.Info("purging soft-deleted Key Vault", "name", kvName)

	poller, err := kvClient.BeginPurgeDeleted(ctx, kvName, location, nil)
	if err != nil {
		return fmt.Errorf("purging Key Vault: %w", err)
	}

	if _, err := poller.PollUntilDone(ctx, nil); err != nil {
		return fmt.Errorf("waiting for Key Vault purge: %w", err)
	}

	return nil
}
