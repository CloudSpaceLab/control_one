package provisioning

import (
	"context"
	"os"
	"strings"
)

type vmwareAdapter struct {
	httpAdapter Adapter
}

func (v *vmwareAdapter) Apply(ctx context.Context, nodeID string, opts Options, metadata map[string]string) (*ApplyResult, error) {
	ensureVMwareMetadata(metadata)
	return v.httpAdapter.Apply(ctx, nodeID, opts, metadata)
}

func (v *vmwareAdapter) RunBaselines(ctx context.Context, nodeID string, opts Options) (*BaselineResult, error) {
	return v.httpAdapter.RunBaselines(ctx, nodeID, opts)
}

func ensureVMwareMetadata(metadata map[string]string) {
	if metadata == nil {
		return
	}

	if datacenter := strings.TrimSpace(metadata["datacenter"]); datacenter == "" {
		if env := strings.TrimSpace(os.Getenv("VMWARE_DATACENTER")); env != "" {
			metadata["datacenter"] = env
		}
	}

	if cluster := strings.TrimSpace(metadata["cluster"]); cluster == "" {
		if env := strings.TrimSpace(os.Getenv("VMWARE_CLUSTER")); env != "" {
			metadata["cluster"] = env
		}
	}

	if datastore := strings.TrimSpace(metadata["datastore"]); datastore == "" {
		if env := strings.TrimSpace(os.Getenv("VMWARE_DATASTORE")); env != "" {
			metadata["datastore"] = env
		}
	}

	if folder := strings.TrimSpace(metadata["folder"]); folder == "" {
		if env := strings.TrimSpace(os.Getenv("VMWARE_FOLDER")); env != "" {
			metadata["folder"] = env
		}
	}

	if network := strings.TrimSpace(metadata["network"]); network == "" {
		if env := strings.TrimSpace(os.Getenv("VMWARE_NETWORK")); env != "" {
			metadata["network"] = env
		}
	}

	if host := strings.TrimSpace(metadata["host"]); host == "" {
		if env := strings.TrimSpace(os.Getenv("VMWARE_HOST")); env != "" {
			metadata["host"] = env
		}
	}
}

