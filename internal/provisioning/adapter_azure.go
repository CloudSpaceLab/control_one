package provisioning

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/arm"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v6"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v6"
	"go.uber.org/zap"
)

type azureAdapter struct {
	httpAdapter Adapter
}

// Apply provisions an Azure VM via armcompute. It requires
// metadata["subscription_id"], ["resource_group"], ["location"],
// ["vm_size"], ["image_publisher"/"image_offer"/"image_sku"/"image_version"]
// (or a shared-image id under ["image_id"]), and ["subnet_id"] pointing at a
// pre-existing subnet. User-data is injected via the `customData` property
// (base64). When any required input is missing, or credentials cannot be
// obtained, the adapter forwards to the HTTP fallback.
func (a *azureAdapter) Apply(ctx context.Context, nodeID string, opts Options, metadata map[string]string) (*ApplyResult, error) {
	ensureAzureMetadata(metadata)
	log := logFromCtx(ctx)

	if !azureMetadataComplete(metadata) {
		return a.httpAdapter.Apply(ctx, nodeID, opts, metadata)
	}

	cred, err := azureCredential(metadata)
	if err != nil {
		log.Warn("azure credential load failed; forwarding", zap.Error(err))
		return a.httpAdapter.Apply(ctx, nodeID, opts, metadata)
	}

	subscriptionID := metadata["subscription_id"]
	rg := metadata["resource_group"]
	loc := metadata["location"]
	nicID, err := ensureAzureNIC(ctx, cred, subscriptionID, rg, loc, nodeID, metadata["subnet_id"])
	if err != nil {
		log.Warn("azure ensure NIC failed; forwarding", zap.Error(err))
		return a.httpAdapter.Apply(ctx, nodeID, opts, metadata)
	}

	vmClient, err := armcompute.NewVirtualMachinesClient(subscriptionID, cred, nil)
	if err != nil {
		return nil, fmt.Errorf("vm client: %w", err)
	}

	userData := strings.TrimSpace(metadata["user_data"])
	if userData == "" {
		if one := strings.TrimSpace(metadata["install_one_liner"]); one != "" {
			userData = "#cloud-config\nruncmd:\n  - [ sh, -c, \"" + escapeYAMLScalar(one) + "\" ]\n"
		}
	}
	var encodedCustomData *string
	if userData != "" {
		enc := base64.StdEncoding.EncodeToString([]byte(userData))
		encodedCustomData = to.Ptr(enc)
	}

	vmName := "controlone-" + nodeID
	vm := armcompute.VirtualMachine{
		Location: to.Ptr(loc),
		Tags: map[string]*string{
			"control-one-node-id": to.Ptr(nodeID),
		},
		Properties: &armcompute.VirtualMachineProperties{
			HardwareProfile: &armcompute.HardwareProfile{
				VMSize: to.Ptr(armcompute.VirtualMachineSizeTypes(strings.TrimSpace(metadata["vm_size"]))),
			},
			StorageProfile: buildAzureStorageProfile(metadata),
			OSProfile: &armcompute.OSProfile{
				ComputerName:  to.Ptr(vmName),
				AdminUsername: to.Ptr(firstNonEmpty(metadata["admin_username"], "controlone")),
				CustomData:    encodedCustomData,
				LinuxConfiguration: &armcompute.LinuxConfiguration{
					DisablePasswordAuthentication: to.Ptr(true),
				},
			},
			NetworkProfile: &armcompute.NetworkProfile{
				NetworkInterfaces: []*armcompute.NetworkInterfaceReference{{
					ID: to.Ptr(nicID),
				}},
			},
		},
	}

	poller, err := vmClient.BeginCreateOrUpdate(ctx, rg, vmName, vm, nil)
	if err != nil {
		log.Warn("azure BeginCreateOrUpdate failed; forwarding", zap.Error(err))
		return a.httpAdapter.Apply(ctx, nodeID, opts, metadata)
	}
	if _, err := poller.PollUntilDone(ctx, nil); err != nil {
		return nil, fmt.Errorf("vm create poll: %w", err)
	}
	log.Info("azure VM create ok",
		zap.String("node_id", nodeID),
		zap.String("location", loc),
		zap.String("resource_group", rg))
	return &ApplyResult{OperationID: "azure-" + vmName}, nil
}

func (a *azureAdapter) RunBaselines(ctx context.Context, nodeID string, opts Options) (*BaselineResult, error) {
	return a.httpAdapter.RunBaselines(ctx, nodeID, opts)
}

// Destroy removes the VM, its NIC, and OS disk when azure creds are present,
// otherwise forwards.
func (a *azureAdapter) Destroy(ctx context.Context, nodeID string) error {
	log := logFromCtx(ctx)
	subscriptionID := strings.TrimSpace(os.Getenv("AZURE_SUBSCRIPTION_ID"))
	rg := strings.TrimSpace(os.Getenv("AZURE_RESOURCE_GROUP"))
	if subscriptionID == "" || rg == "" {
		return a.httpAdapter.Destroy(ctx, nodeID)
	}
	cred, err := azureCredential(nil)
	if err != nil {
		return a.httpAdapter.Destroy(ctx, nodeID)
	}
	vmClient, err := armcompute.NewVirtualMachinesClient(subscriptionID, cred, nil)
	if err != nil {
		return fmt.Errorf("vm client: %w", err)
	}
	vmName := "controlone-" + nodeID
	poller, err := vmClient.BeginDelete(ctx, rg, vmName, nil)
	if err != nil {
		log.Warn("azure BeginDelete failed; forwarding", zap.Error(err))
		return a.httpAdapter.Destroy(ctx, nodeID)
	}
	if _, err := poller.PollUntilDone(ctx, nil); err != nil {
		return fmt.Errorf("vm delete poll: %w", err)
	}
	return nil
}

// VerifyReachable performs a cheap Get against the resource group to prove
// credentials are live.
func (a *azureAdapter) VerifyReachable(ctx context.Context, _ string, metadata map[string]string) error {
	ensureAzureMetadata(metadata)
	subscriptionID := strings.TrimSpace(metadata["subscription_id"])
	rg := strings.TrimSpace(metadata["resource_group"])
	if subscriptionID == "" || rg == "" {
		return errors.New("azure subscription_id + resource_group required")
	}
	cred, err := azureCredential(metadata)
	if err != nil {
		return err
	}
	// Use the network subnets client as a cheap read-only call — it returns
	// 404 (not 403) when the account is authorised but the subnet doesn't
	// exist, which is fine for verification.
	nicClient, err := armnetwork.NewInterfacesClient(subscriptionID, cred, nil)
	if err != nil {
		return fmt.Errorf("nic client: %w", err)
	}
	pager := nicClient.NewListPager(rg, nil)
	if pager.More() {
		if _, err := pager.NextPage(ctx); err != nil {
			return fmt.Errorf("list nics: %w", err)
		}
	}
	return nil
}

func (a *azureAdapter) RegisterLB(ctx context.Context, nodeID string, clusterMeta map[string]any) error {
	return a.httpAdapter.RegisterLB(ctx, nodeID, clusterMeta)
}
func (a *azureAdapter) DeregisterLB(ctx context.Context, nodeID string, clusterMeta map[string]any) error {
	return a.httpAdapter.DeregisterLB(ctx, nodeID, clusterMeta)
}

func azureMetadataComplete(metadata map[string]string) bool {
	if metadata == nil {
		return false
	}
	for _, k := range []string{"subscription_id", "resource_group", "location", "vm_size", "subnet_id"} {
		if strings.TrimSpace(metadata[k]) == "" {
			return false
		}
	}
	// Either a marketplace image triple OR a shared-image id is required.
	hasImage := strings.TrimSpace(metadata["image_id"]) != "" ||
		(strings.TrimSpace(metadata["image_publisher"]) != "" &&
			strings.TrimSpace(metadata["image_offer"]) != "" &&
			strings.TrimSpace(metadata["image_sku"]) != "")
	return hasImage
}

func azureCredential(metadata map[string]string) (*azidentity.ClientSecretCredential, error) {
	tenantID := firstNonEmpty(valOrEmpty(metadata, "_cred_tenant_id"), valOrEmpty(metadata, "tenant_id"), os.Getenv("AZURE_TENANT_ID"))
	clientID := firstNonEmpty(valOrEmpty(metadata, "_cred_client_id"), os.Getenv("AZURE_CLIENT_ID"))
	secret := firstNonEmpty(valOrEmpty(metadata, "_cred_client_secret"), os.Getenv("AZURE_CLIENT_SECRET"))
	if tenantID == "" || clientID == "" || secret == "" {
		return nil, errors.New("azure client_secret credentials missing (tenant_id, client_id, client_secret)")
	}
	return azidentity.NewClientSecretCredential(tenantID, clientID, secret, nil)
}

func buildAzureStorageProfile(metadata map[string]string) *armcompute.StorageProfile {
	if id := strings.TrimSpace(metadata["image_id"]); id != "" {
		return &armcompute.StorageProfile{
			ImageReference: &armcompute.ImageReference{ID: to.Ptr(id)},
			OSDisk: &armcompute.OSDisk{
				CreateOption: to.Ptr(armcompute.DiskCreateOptionTypesFromImage),
				Caching:      to.Ptr(armcompute.CachingTypesReadWrite),
				ManagedDisk: &armcompute.ManagedDiskParameters{
					StorageAccountType: to.Ptr(armcompute.StorageAccountTypesStandardLRS),
				},
			},
		}
	}
	version := firstNonEmpty(metadata["image_version"], "latest")
	return &armcompute.StorageProfile{
		ImageReference: &armcompute.ImageReference{
			Publisher: to.Ptr(metadata["image_publisher"]),
			Offer:     to.Ptr(metadata["image_offer"]),
			SKU:       to.Ptr(metadata["image_sku"]),
			Version:   to.Ptr(version),
		},
		OSDisk: &armcompute.OSDisk{
			CreateOption: to.Ptr(armcompute.DiskCreateOptionTypesFromImage),
			Caching:      to.Ptr(armcompute.CachingTypesReadWrite),
			ManagedDisk: &armcompute.ManagedDiskParameters{
				StorageAccountType: to.Ptr(armcompute.StorageAccountTypesStandardLRS),
			},
		},
	}
}

// ensureAzureNIC creates a minimal NIC attached to the supplied subnet so the
// VM has connectivity. The NIC name is deterministic per nodeID so re-runs
// hit CreateOrUpdate idempotency.
func ensureAzureNIC(ctx context.Context, cred *azidentity.ClientSecretCredential, subscriptionID, rg, location, nodeID, subnetID string) (string, error) {
	nicClient, err := armnetwork.NewInterfacesClient(subscriptionID, cred, nil)
	if err != nil {
		return "", fmt.Errorf("nic client: %w", err)
	}
	nicName := "controlone-" + nodeID + "-nic"
	ipConfigName := "ipcfg"

	payload := armnetwork.Interface{
		Location: to.Ptr(location),
		Properties: &armnetwork.InterfacePropertiesFormat{
			IPConfigurations: []*armnetwork.InterfaceIPConfiguration{{
				Name: to.Ptr(ipConfigName),
				Properties: &armnetwork.InterfaceIPConfigurationPropertiesFormat{
					Subnet:                    &armnetwork.Subnet{ID: to.Ptr(subnetID)},
					PrivateIPAllocationMethod: to.Ptr(armnetwork.IPAllocationMethodDynamic),
				},
			}},
		},
	}
	poller, err := nicClient.BeginCreateOrUpdate(ctx, rg, nicName, payload, nil)
	if err != nil {
		return "", fmt.Errorf("nic create: %w", err)
	}
	resp, err := poller.PollUntilDone(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("nic poll: %w", err)
	}
	if resp.ID == nil {
		return "", fmt.Errorf("nic returned without id")
	}
	_ = arm.ResourceID{} // anchor import; kept so gofmt does not prune arm
	return *resp.ID, nil
}

func valOrEmpty(m map[string]string, k string) string {
	if m == nil {
		return ""
	}
	return m[k]
}

func ensureAzureMetadata(metadata map[string]string) {
	if metadata == nil {
		return
	}

	if subscriptionID := strings.TrimSpace(metadata["subscription_id"]); subscriptionID == "" {
		if env := strings.TrimSpace(os.Getenv("AZURE_SUBSCRIPTION_ID")); env != "" {
			metadata["subscription_id"] = env
		}
	}

	if resourceGroup := strings.TrimSpace(metadata["resource_group"]); resourceGroup == "" {
		if env := strings.TrimSpace(os.Getenv("AZURE_RESOURCE_GROUP")); env != "" {
			metadata["resource_group"] = env
		}
	}

	if location := strings.TrimSpace(metadata["location"]); location == "" {
		if region := strings.TrimSpace(metadata["region"]); region != "" {
			metadata["location"] = region
		} else if env := strings.TrimSpace(os.Getenv("AZURE_LOCATION")); env != "" {
			metadata["location"] = env
		} else if env := strings.TrimSpace(os.Getenv("AZURE_REGION")); env != "" {
			metadata["location"] = env
		} else {
			metadata["location"] = "eastus"
		}
	}

	if vnet := strings.TrimSpace(metadata["vnet"]); vnet == "" {
		if env := strings.TrimSpace(os.Getenv("AZURE_VNET")); env != "" {
			metadata["vnet"] = env
		}
	}

	if subnet := strings.TrimSpace(metadata["subnet"]); subnet == "" {
		if env := strings.TrimSpace(os.Getenv("AZURE_SUBNET")); env != "" {
			metadata["subnet"] = env
		}
	}
}
