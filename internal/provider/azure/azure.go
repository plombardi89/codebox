package azure

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v6"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v6"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/resources/armresources"

	"github.com/plombardi89/codebox/internal/logging"
	"github.com/plombardi89/codebox/internal/provider"
	"github.com/plombardi89/codebox/internal/state"
)

func init() {
	provider.Register("azure", &azureProvider{})
}

// azureProvider implements provider.Provider using the Azure SDK.
type azureProvider struct{}

func (a *azureProvider) Name() string {
	return "azure"
}

// optOrDefault returns opts[key] if present and non-empty, otherwise def.
func optOrDefault(opts map[string]string, key, def string) string {
	if v, ok := opts[key]; ok && v != "" {
		return v
	}
	return def
}

// subscriptionID returns the Azure subscription ID from the environment or an
// error when the variable is not set.
func subscriptionID() (string, error) {
	id := os.Getenv("AZURE_SUBSCRIPTION_ID")
	if id == "" {
		return "", fmt.Errorf("AZURE_SUBSCRIPTION_ID environment variable is not set")
	}
	return id, nil
}

func (a *azureProvider) Up(ctx context.Context, st *state.BoxState, pubKey string, opts map[string]string) (*state.BoxState, error) {
	log := logging.Get()

	subID, err := subscriptionID()
	if err != nil {
		return nil, err
	}

	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, fmt.Errorf("creating Azure credential: %w", err)
	}

	vmSize := optOrDefault(opts, "vm-size", "Standard_B2s")
	location := optOrDefault(opts, "location", "westeurope")
	rgName := fmt.Sprintf("codebox-%s", st.Name)

	// Idempotency: if we already have a VM, check its state.
	if rg, ok := st.ProviderData["resource_group"]; ok {
		if vmName, ok2 := st.ProviderData["vm_name"]; ok2 {
			log.Debug("existing VM found in state", "resource_group", rg, "vm_name", vmName)
			vmClient, err := armcompute.NewVirtualMachinesClient(subID, cred, nil)
			if err != nil {
				return nil, fmt.Errorf("creating VM client: %w", err)
			}

			resp, err := vmClient.InstanceView(ctx, rg, vmName, nil)
			if err != nil {
				return nil, fmt.Errorf("getting VM instance view: %w", err)
			}

			powerState := extractPowerState(resp.Statuses)
			switch powerState {
			case "running":
				log.Debug("VM already running", "name", st.Name)
				// Re-fetch public IP for current address.
				ip, err := fetchPublicIP(ctx, subID, cred, rg)
				if err != nil {
					return nil, err
				}
				st.IP = ip
				st.SSHPort = 2222
				st.Status = "up"
				st.UpdatedAt = time.Now()
				return st, nil

			case "deallocated":
				log.Info("starting deallocated VM", "name", st.Name)
				poller, err := vmClient.BeginStart(ctx, rg, vmName, nil)
				if err != nil {
					return nil, fmt.Errorf("starting VM: %w", err)
				}
				if _, err := poller.PollUntilDone(ctx, nil); err != nil {
					return nil, fmt.Errorf("waiting for VM start: %w", err)
				}
				ip, err := fetchPublicIP(ctx, subID, cred, rg)
				if err != nil {
					return nil, err
				}
				st.IP = ip
				st.SSHPort = 2222
				st.Status = "up"
				st.UpdatedAt = time.Now()
				return st, nil

			default:
				return nil, fmt.Errorf("VM is in unexpected power state: %s", powerState)
			}
		}
	}

	// No existing VM -- create from scratch.
	if st.ProviderData == nil {
		st.ProviderData = make(map[string]string)
	}

	// 1. Create resource group.
	log.Info("creating resource group", "name", rgName, "location", location)
	rgClient, err := armresources.NewResourceGroupsClient(subID, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("creating resource groups client: %w", err)
	}
	_, err = rgClient.CreateOrUpdate(ctx, rgName, armresources.ResourceGroup{
		Location: to.Ptr(location),
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("creating resource group: %w", err)
	}

	// 2. Create NSG with SSH rule on port 2222.
	log.Info("creating NSG", "name", "codebox", "resource_group", rgName)
	nsgClient, err := armnetwork.NewSecurityGroupsClient(subID, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("creating NSG client: %w", err)
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
						SourceAddressPrefix:      to.Ptr("*"),
						SourcePortRange:          to.Ptr("*"),
						DestinationAddressPrefix: to.Ptr("*"),
						DestinationPortRange:     to.Ptr("2222"),
					},
				},
			},
		},
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("creating NSG: %w", err)
	}
	nsgResp, err := nsgPoller.PollUntilDone(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("waiting for NSG creation: %w", err)
	}

	// 3. Create VNet with subnet.
	log.Info("creating VNet", "name", "codebox", "resource_group", rgName)
	vnetClient, err := armnetwork.NewVirtualNetworksClient(subID, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("creating VNet client: %w", err)
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
		return nil, fmt.Errorf("creating VNet: %w", err)
	}
	vnetResp, err := vnetPoller.PollUntilDone(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("waiting for VNet creation: %w", err)
	}

	// Locate subnet ID from VNet response.
	var subnetID string
	if vnetResp.Properties != nil && len(vnetResp.Properties.Subnets) > 0 {
		subnetID = *vnetResp.Properties.Subnets[0].ID
	} else {
		return nil, fmt.Errorf("subnet not found in VNet response")
	}

	// 4. Create public IP.
	log.Info("creating public IP", "name", "codebox", "resource_group", rgName)
	pipClient, err := armnetwork.NewPublicIPAddressesClient(subID, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("creating public IP client: %w", err)
	}
	pipPoller, err := pipClient.BeginCreateOrUpdate(ctx, rgName, "codebox", armnetwork.PublicIPAddress{
		Location: to.Ptr(location),
		Properties: &armnetwork.PublicIPAddressPropertiesFormat{
			PublicIPAllocationMethod: to.Ptr(armnetwork.IPAllocationMethodStatic),
		},
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("creating public IP: %w", err)
	}
	pipResp, err := pipPoller.PollUntilDone(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("waiting for public IP creation: %w", err)
	}

	// 5. Create NIC attached to subnet + public IP + NSG.
	log.Info("creating NIC", "name", "codebox", "resource_group", rgName)
	nicClient, err := armnetwork.NewInterfacesClient(subID, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("creating NIC client: %w", err)
	}
	nicPoller, err := nicClient.BeginCreateOrUpdate(ctx, rgName, "codebox", armnetwork.Interface{
		Location: to.Ptr(location),
		Properties: &armnetwork.InterfacePropertiesFormat{
			NetworkSecurityGroup: &armnetwork.SecurityGroup{
				ID: nsgResp.ID,
			},
			IPConfigurations: []*armnetwork.InterfaceIPConfiguration{
				{
					Name: to.Ptr("ipconfig1"),
					Properties: &armnetwork.InterfaceIPConfigurationPropertiesFormat{
						Subnet: &armnetwork.Subnet{
							ID: to.Ptr(subnetID),
						},
						PublicIPAddress: &armnetwork.PublicIPAddress{
							ID: pipResp.ID,
						},
						PrivateIPAllocationMethod: to.Ptr(armnetwork.IPAllocationMethodDynamic),
					},
				},
			},
		},
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("creating NIC: %w", err)
	}
	nicResp, err := nicPoller.PollUntilDone(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("waiting for NIC creation: %w", err)
	}

	// 6. Create VM.
	log.Info("creating VM", "name", "codebox", "resource_group", rgName, "vm_size", vmSize)
	vmClient, err := armcompute.NewVirtualMachinesClient(subID, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("creating VM client: %w", err)
	}

	userData := base64.StdEncoding.EncodeToString([]byte(optOrDefault(opts, "user-data", "")))

	vmPoller, err := vmClient.BeginCreateOrUpdate(ctx, rgName, "codebox", armcompute.VirtualMachine{
		Location: to.Ptr(location),
		Properties: &armcompute.VirtualMachineProperties{
			HardwareProfile: &armcompute.HardwareProfile{
				VMSize: to.Ptr(armcompute.VirtualMachineSizeTypes(vmSize)),
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
				AdminUsername: to.Ptr("dev"),
				LinuxConfiguration: &armcompute.LinuxConfiguration{
					DisablePasswordAuthentication: to.Ptr(true),
					SSH: &armcompute.SSHConfiguration{
						PublicKeys: []*armcompute.SSHPublicKey{
							{
								Path:    to.Ptr("/home/dev/.ssh/authorized_keys"),
								KeyData: to.Ptr(pubKey),
							},
						},
					},
				},
			},
			NetworkProfile: &armcompute.NetworkProfile{
				NetworkInterfaces: []*armcompute.NetworkInterfaceReference{
					{
						ID: nicResp.ID,
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
		return nil, fmt.Errorf("creating VM: %w", err)
	}

	// 7. Wait for provisioning.
	if _, err := vmPoller.PollUntilDone(ctx, nil); err != nil {
		return nil, fmt.Errorf("waiting for VM creation: %w", err)
	}

	// 8. Re-fetch public IP to get assigned address.
	ip, err := fetchPublicIP(ctx, subID, cred, rgName)
	if err != nil {
		return nil, err
	}

	// 9. Set state.
	st.IP = ip
	st.SSHPort = 2222
	st.Image = "Fedora-Cloud-43-x64"
	st.Status = "up"
	st.ProviderData["resource_group"] = rgName
	st.ProviderData["vm_name"] = "codebox"
	st.UpdatedAt = time.Now()
	log.Debug("VM created", "resource_group", rgName, "ip", st.IP)

	return st, nil
}

func (a *azureProvider) Down(ctx context.Context, st *state.BoxState) (*state.BoxState, error) {
	log := logging.Get()

	subID, err := subscriptionID()
	if err != nil {
		return nil, err
	}

	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, fmt.Errorf("creating Azure credential: %w", err)
	}

	rg := st.ProviderData["resource_group"]
	vmName := st.ProviderData["vm_name"]

	log.Info("deallocating VM", "resource_group", rg, "vm_name", vmName)
	vmClient, err := armcompute.NewVirtualMachinesClient(subID, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("creating VM client: %w", err)
	}

	poller, err := vmClient.BeginDeallocate(ctx, rg, vmName, nil)
	if err != nil {
		return nil, fmt.Errorf("deallocating VM: %w", err)
	}
	if _, err := poller.PollUntilDone(ctx, nil); err != nil {
		return nil, fmt.Errorf("waiting for VM deallocation: %w", err)
	}

	st.Status = "down"
	st.UpdatedAt = time.Now()
	return st, nil
}

func (a *azureProvider) Delete(ctx context.Context, st *state.BoxState) error {
	log := logging.Get()

	subID, err := subscriptionID()
	if err != nil {
		return err
	}

	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return fmt.Errorf("creating Azure credential: %w", err)
	}

	rg := st.ProviderData["resource_group"]

	log.Info("deleting resource group", "name", rg)
	rgClient, err := armresources.NewResourceGroupsClient(subID, cred, nil)
	if err != nil {
		return fmt.Errorf("creating resource groups client: %w", err)
	}

	poller, err := rgClient.BeginDelete(ctx, rg, nil)
	if err != nil {
		return fmt.Errorf("deleting resource group: %w", err)
	}
	if _, err := poller.PollUntilDone(ctx, nil); err != nil {
		return fmt.Errorf("waiting for resource group deletion: %w", err)
	}

	log.Info("resource group deleted", "name", rg)
	return nil
}

func (a *azureProvider) Status(ctx context.Context, st *state.BoxState) (*state.BoxState, error) {
	subID, err := subscriptionID()
	if err != nil {
		return nil, err
	}

	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return nil, fmt.Errorf("creating Azure credential: %w", err)
	}

	rg := st.ProviderData["resource_group"]
	vmName := st.ProviderData["vm_name"]

	vmClient, err := armcompute.NewVirtualMachinesClient(subID, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("creating VM client: %w", err)
	}

	resp, err := vmClient.InstanceView(ctx, rg, vmName, nil)
	if err != nil {
		return nil, fmt.Errorf("getting VM instance view: %w", err)
	}

	powerState := extractPowerState(resp.Statuses)
	switch powerState {
	case "running":
		st.Status = "up"
	case "deallocated":
		st.Status = "down"
	default:
		st.Status = "unknown"
	}

	if st.Status == "up" {
		ip, err := fetchPublicIP(ctx, subID, cred, rg)
		if err != nil {
			return nil, err
		}
		st.IP = ip
	}

	st.UpdatedAt = time.Now()
	return st, nil
}

// extractPowerState finds the PowerState from a list of instance view statuses.
// Azure power state codes look like "PowerState/running", "PowerState/deallocated", etc.
func extractPowerState(statuses []*armcompute.InstanceViewStatus) string {
	for _, s := range statuses {
		if s.Code == nil {
			continue
		}
		if strings.HasPrefix(*s.Code, "PowerState/") {
			return strings.TrimPrefix(*s.Code, "PowerState/")
		}
	}
	return "unknown"
}

// fetchPublicIP retrieves the current IP address of the "codebox" public IP
// resource in the given resource group.
func fetchPublicIP(ctx context.Context, subID string, cred *azidentity.DefaultAzureCredential, rg string) (string, error) {
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
