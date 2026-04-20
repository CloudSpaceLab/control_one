package provisioning

import (
	"context"
	"os"
	"strings"
)

type libvirtAdapter struct {
	httpAdapter Adapter
}

func (l *libvirtAdapter) Apply(ctx context.Context, nodeID string, opts Options, metadata map[string]string) (*ApplyResult, error) {
	ensureLibvirtMetadata(metadata)
	return l.httpAdapter.Apply(ctx, nodeID, opts, metadata)
}

func (l *libvirtAdapter) RunBaselines(ctx context.Context, nodeID string, opts Options) (*BaselineResult, error) {
	return l.httpAdapter.RunBaselines(ctx, nodeID, opts)
}

// Destroy for libvirt delegates to the provisioning backend which destroys +
// undefines the domain via go-libvirt. In local-dev this is usually the
// simulated path.
func (l *libvirtAdapter) Destroy(ctx context.Context, nodeID string) error {
	return l.httpAdapter.Destroy(ctx, nodeID)
}

// RegisterLB for libvirt is a no-op at the adapter level — libvirt deployments
// don't have a native cluster load balancer. We forward so the backend can
// still record the intent (e.g. HAProxy updates) if configured; missing
// backend endpoints degrade to log-WARN.
func (l *libvirtAdapter) RegisterLB(ctx context.Context, nodeID string, clusterMeta map[string]any) error {
	return l.httpAdapter.RegisterLB(ctx, nodeID, clusterMeta)
}

// DeregisterLB for libvirt mirrors RegisterLB — forward + degrade.
func (l *libvirtAdapter) DeregisterLB(ctx context.Context, nodeID string, clusterMeta map[string]any) error {
	return l.httpAdapter.DeregisterLB(ctx, nodeID, clusterMeta)
}

func ensureLibvirtMetadata(metadata map[string]string) {
	if metadata == nil {
		return
	}

	if pool := strings.TrimSpace(metadata["pool"]); pool == "" {
		if env := strings.TrimSpace(os.Getenv("LIBVIRT_POOL")); env != "" {
			metadata["pool"] = env
		} else {
			metadata["pool"] = "default"
		}
	}

	if network := strings.TrimSpace(metadata["network"]); network == "" {
		if env := strings.TrimSpace(os.Getenv("LIBVIRT_NETWORK")); env != "" {
			metadata["network"] = env
		} else {
			metadata["network"] = "default"
		}
	}

	if image := strings.TrimSpace(metadata["image"]); image == "" {
		if env := strings.TrimSpace(os.Getenv("LIBVIRT_IMAGE")); env != "" {
			metadata["image"] = env
		}
	}

	if cpu := strings.TrimSpace(metadata["cpu"]); cpu == "" {
		if env := strings.TrimSpace(os.Getenv("LIBVIRT_CPU")); env != "" {
			metadata["cpu"] = env
		} else {
			metadata["cpu"] = "2"
		}
	}

	if memory := strings.TrimSpace(metadata["memory"]); memory == "" {
		if env := strings.TrimSpace(os.Getenv("LIBVIRT_MEMORY")); env != "" {
			metadata["memory"] = env
		} else {
			metadata["memory"] = "2048"
		}
	}

	if uri := strings.TrimSpace(metadata["uri"]); uri == "" {
		if env := strings.TrimSpace(os.Getenv("LIBVIRT_URI")); env != "" {
			metadata["uri"] = env
		} else {
			metadata["uri"] = "qemu:///system"
		}
	}
}
