package provisioning

import (
	"context"
	"os"
	"strings"
)

type azureAdapter struct {
	httpAdapter Adapter
}

func (a *azureAdapter) Apply(ctx context.Context, nodeID string, opts Options, metadata map[string]string) (*ApplyResult, error) {
	ensureAzureMetadata(metadata)
	return a.httpAdapter.Apply(ctx, nodeID, opts, metadata)
}

func (a *azureAdapter) RunBaselines(ctx context.Context, nodeID string, opts Options) (*BaselineResult, error) {
	return a.httpAdapter.RunBaselines(ctx, nodeID, opts)
}

// Destroy for Azure delegates to the provisioning backend which issues
// VirtualMachines.Delete + NIC/disk cleanup via armcompute/armnetwork SDKs.
func (a *azureAdapter) Destroy(ctx context.Context, nodeID string) error {
	return a.httpAdapter.Destroy(ctx, nodeID)
}

// RegisterLB for Azure maps to LoadBalancerBackendAddressPoolsClient Add when
// clusterMeta["lb_backend_pool_id"] is set.
func (a *azureAdapter) RegisterLB(ctx context.Context, nodeID string, clusterMeta map[string]any) error {
	return a.httpAdapter.RegisterLB(ctx, nodeID, clusterMeta)
}

// DeregisterLB for Azure maps to LoadBalancerBackendAddressPoolsClient Remove.
func (a *azureAdapter) DeregisterLB(ctx context.Context, nodeID string, clusterMeta map[string]any) error {
	return a.httpAdapter.DeregisterLB(ctx, nodeID, clusterMeta)
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
