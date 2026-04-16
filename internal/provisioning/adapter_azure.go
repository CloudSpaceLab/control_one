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
