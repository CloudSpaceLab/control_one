package provisioning

import (
	"context"
	"fmt"
	"testing"
)

type providerTestStubAdapter struct {
	metadata       map[string]string
	destroyCalls   []string
	lbRegisterCall map[string]any
	lbDeregCall    map[string]any
}

func (s *providerTestStubAdapter) Apply(ctx context.Context, nodeID string, opts Options, metadata map[string]string) (*ApplyResult, error) {
	s.metadata = metadata
	return &ApplyResult{OperationID: "stub-op"}, nil
}

func (s *providerTestStubAdapter) RunBaselines(ctx context.Context, nodeID string, opts Options) (*BaselineResult, error) {
	return &BaselineResult{Notes: "stub"}, nil
}

func (s *providerTestStubAdapter) Destroy(_ context.Context, nodeID string) error {
	s.destroyCalls = append(s.destroyCalls, nodeID)
	return nil
}

func (s *providerTestStubAdapter) RegisterLB(_ context.Context, _ string, clusterMeta map[string]any) error {
	s.lbRegisterCall = clusterMeta
	return nil
}

func (s *providerTestStubAdapter) DeregisterLB(_ context.Context, _ string, clusterMeta map[string]any) error {
	s.lbDeregCall = clusterMeta
	return nil
}

func TestAzureAdapterEnsuresMetadata(t *testing.T) {
	t.Setenv("AZURE_SUBSCRIPTION_ID", "sub-123")
	t.Setenv("AZURE_RESOURCE_GROUP", "rg-test")
	t.Setenv("AZURE_LOCATION", "westus2")
	t.Setenv("AZURE_VNET", "vnet-test")
	t.Setenv("AZURE_SUBNET", "subnet-test")

	capture := &providerTestStubAdapter{}
	adapter := &azureAdapter{httpAdapter: capture}

	metadata := map[string]string{}
	if _, err := adapter.Apply(context.Background(), "node-1", Options{Template: "demo"}, metadata); err != nil {
		t.Fatalf("azure adapter apply failed: %v", err)
	}

	if capture.metadata["subscription_id"] != "sub-123" {
		t.Errorf("expected subscription_id, got %v", capture.metadata)
	}
	if capture.metadata["resource_group"] != "rg-test" {
		t.Errorf("expected resource_group, got %v", capture.metadata)
	}
	if capture.metadata["location"] != "westus2" {
		t.Errorf("expected location, got %v", capture.metadata)
	}
	if capture.metadata["vnet"] != "vnet-test" {
		t.Errorf("expected vnet, got %v", capture.metadata)
	}
	if capture.metadata["subnet"] != "subnet-test" {
		t.Errorf("expected subnet, got %v", capture.metadata)
	}
}

func TestAzureAdapterDefaultsLocation(t *testing.T) {
	capture := &providerTestStubAdapter{}
	adapter := &azureAdapter{httpAdapter: capture}

	metadata := map[string]string{}
	if _, err := adapter.Apply(context.Background(), "node-1", Options{Template: "demo"}, metadata); err != nil {
		t.Fatalf("azure adapter apply failed: %v", err)
	}

	if capture.metadata["location"] != "eastus" {
		t.Errorf("expected default location eastus, got %v", capture.metadata["location"])
	}
}

func TestVMwareAdapterEnsuresMetadata(t *testing.T) {
	t.Setenv("VMWARE_DATACENTER", "dc-1")
	t.Setenv("VMWARE_CLUSTER", "cluster-1")
	t.Setenv("VMWARE_DATASTORE", "ds-1")
	t.Setenv("VMWARE_FOLDER", "/vm/folder")
	t.Setenv("VMWARE_NETWORK", "network-1")
	t.Setenv("VMWARE_HOST", "host-1")

	capture := &providerTestStubAdapter{}
	adapter := &vmwareAdapter{httpAdapter: capture}

	metadata := map[string]string{}
	if _, err := adapter.Apply(context.Background(), "node-1", Options{Template: "demo"}, metadata); err != nil {
		t.Fatalf("vmware adapter apply failed: %v", err)
	}

	if capture.metadata["datacenter"] != "dc-1" {
		t.Errorf("expected datacenter, got %v", capture.metadata)
	}
	if capture.metadata["cluster"] != "cluster-1" {
		t.Errorf("expected cluster, got %v", capture.metadata)
	}
	if capture.metadata["datastore"] != "ds-1" {
		t.Errorf("expected datastore, got %v", capture.metadata)
	}
	if capture.metadata["folder"] != "/vm/folder" {
		t.Errorf("expected folder, got %v", capture.metadata)
	}
	if capture.metadata["network"] != "network-1" {
		t.Errorf("expected network, got %v", capture.metadata)
	}
	if capture.metadata["host"] != "host-1" {
		t.Errorf("expected host, got %v", capture.metadata)
	}
}

func TestLibvirtAdapterEnsuresMetadata(t *testing.T) {
	t.Setenv("LIBVIRT_POOL", "custom-pool")
	t.Setenv("LIBVIRT_NETWORK", "custom-network")
	t.Setenv("LIBVIRT_IMAGE", "ubuntu-22.04.qcow2")
	t.Setenv("LIBVIRT_CPU", "4")
	t.Setenv("LIBVIRT_MEMORY", "4096")
	t.Setenv("LIBVIRT_URI", "qemu+ssh://user@host/system")

	capture := &providerTestStubAdapter{}
	adapter := &libvirtAdapter{httpAdapter: capture}

	metadata := map[string]string{}
	if _, err := adapter.Apply(context.Background(), "node-1", Options{Template: "demo"}, metadata); err != nil {
		t.Fatalf("libvirt adapter apply failed: %v", err)
	}

	if capture.metadata["pool"] != "custom-pool" {
		t.Errorf("expected pool, got %v", capture.metadata)
	}
	if capture.metadata["network"] != "custom-network" {
		t.Errorf("expected network, got %v", capture.metadata)
	}
	if capture.metadata["image"] != "ubuntu-22.04.qcow2" {
		t.Errorf("expected image, got %v", capture.metadata)
	}
	if capture.metadata["cpu"] != "4" {
		t.Errorf("expected cpu, got %v", capture.metadata)
	}
	if capture.metadata["memory"] != "4096" {
		t.Errorf("expected memory, got %v", capture.metadata)
	}
	if capture.metadata["uri"] != "qemu+ssh://user@host/system" {
		t.Errorf("expected uri, got %v", capture.metadata)
	}
}

func TestLibvirtAdapterDefaults(t *testing.T) {
	capture := &providerTestStubAdapter{}
	adapter := &libvirtAdapter{httpAdapter: capture}

	metadata := map[string]string{}
	if _, err := adapter.Apply(context.Background(), "node-1", Options{Template: "demo"}, metadata); err != nil {
		t.Fatalf("libvirt adapter apply failed: %v", err)
	}

	if capture.metadata["pool"] != "default" {
		t.Errorf("expected default pool, got %v", capture.metadata["pool"])
	}
	if capture.metadata["network"] != "default" {
		t.Errorf("expected default network, got %v", capture.metadata["network"])
	}
	if capture.metadata["cpu"] != "2" {
		t.Errorf("expected default cpu, got %v", capture.metadata["cpu"])
	}
	if capture.metadata["memory"] != "2048" {
		t.Errorf("expected default memory, got %v", capture.metadata["memory"])
	}
	if capture.metadata["uri"] != "qemu:///system" {
		t.Errorf("expected default uri, got %v", capture.metadata["uri"])
	}
}

func TestProviderAdaptersPreserveExistingMetadata(t *testing.T) {
	tests := []struct {
		name     string
		adapter  Adapter
		metadata map[string]string
		check    func(map[string]string) error
	}{
		{
			name:    "azure preserves existing location",
			adapter: &azureAdapter{httpAdapter: &stubAdapter{}},
			metadata: map[string]string{
				"location": "custom-location",
			},
			check: func(m map[string]string) error {
				if m["location"] != "custom-location" {
					return fmt.Errorf("expected custom-location, got %s", m["location"])
				}
				return nil
			},
		},
		{
			name:    "vmware preserves existing datacenter",
			adapter: &vmwareAdapter{httpAdapter: &stubAdapter{}},
			metadata: map[string]string{
				"datacenter": "custom-dc",
			},
			check: func(m map[string]string) error {
				if m["datacenter"] != "custom-dc" {
					return fmt.Errorf("expected custom-dc, got %s", m["datacenter"])
				}
				return nil
			},
		},
		{
			name:    "libvirt preserves existing pool",
			adapter: &libvirtAdapter{httpAdapter: &stubAdapter{}},
			metadata: map[string]string{
				"pool": "custom-pool",
			},
			check: func(m map[string]string) error {
				if m["pool"] != "custom-pool" {
					return fmt.Errorf("expected custom-pool, got %s", m["pool"])
				}
				return nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			capture := &providerTestStubAdapter{}
			var testAdapter Adapter
			switch tt.adapter.(type) {
			case *azureAdapter:
				testAdapter = &azureAdapter{httpAdapter: capture}
			case *vmwareAdapter:
				testAdapter = &vmwareAdapter{httpAdapter: capture}
			case *libvirtAdapter:
				testAdapter = &libvirtAdapter{httpAdapter: capture}
			default:
				t.Fatalf("unknown adapter type")
			}

			if _, err := testAdapter.Apply(context.Background(), "node-1", Options{Template: "demo"}, tt.metadata); err != nil {
				t.Fatalf("adapter apply failed: %v", err)
			}

			if err := tt.check(capture.metadata); err != nil {
				t.Error(err)
			}
		})
	}
}
