package provisioning

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/vmware/govmomi"
	"github.com/vmware/govmomi/find"
	"github.com/vmware/govmomi/object"
	"github.com/vmware/govmomi/vim25/types"
	"go.uber.org/zap"
)

type vmwareAdapter struct {
	httpAdapter Adapter
}

// Apply clones a VM from a pre-existing template and injects cloud-init via
// the guestinfo custom properties that open-vm-tools / cloud-init pick up at
// first boot. When vCenter cannot be reached we fall back to the legacy HTTP
// adapter so development environments without real vSphere access keep
// working.
func (v *vmwareAdapter) Apply(ctx context.Context, nodeID string, opts Options, metadata map[string]string) (*ApplyResult, error) {
	ensureVMwareMetadata(metadata)
	log := logFromCtx(ctx)

	endpoint := pickVMwareEndpoint(metadata)
	if endpoint == "" {
		return v.httpAdapter.Apply(ctx, nodeID, opts, metadata)
	}

	if _, err := vmwareApply(ctx, log, endpoint, nodeID, opts, metadata); err != nil {
		log.Warn("vmware native apply failed; falling back to http adapter",
			zap.String("endpoint", endpoint),
			zap.Error(err))
		return v.httpAdapter.Apply(ctx, nodeID, opts, metadata)
	}
	return &ApplyResult{OperationID: fmt.Sprintf("vmware-%s", nodeID)}, nil
}

func (v *vmwareAdapter) RunBaselines(ctx context.Context, nodeID string, opts Options) (*BaselineResult, error) {
	return v.httpAdapter.RunBaselines(ctx, nodeID, opts)
}

// Destroy powers off + removes the VM via govmomi when vCenter is reachable.
func (v *vmwareAdapter) Destroy(ctx context.Context, nodeID string) error {
	log := logFromCtx(ctx)
	endpoint := strings.TrimSpace(os.Getenv("VMWARE_URL"))
	if endpoint == "" {
		return v.httpAdapter.Destroy(ctx, nodeID)
	}
	if err := vmwareDestroy(ctx, log, endpoint, nodeID); err != nil {
		log.Warn("vmware destroy failed; forwarding to http adapter", zap.Error(err))
		return v.httpAdapter.Destroy(ctx, nodeID)
	}
	return nil
}

// VerifyReachable satisfies the Verifier interface — login + logout against
// vCenter to prove credentials are live.
func (v *vmwareAdapter) VerifyReachable(ctx context.Context, _ string, metadata map[string]string) error {
	endpoint := pickVMwareEndpoint(metadata)
	if endpoint == "" {
		return errors.New("vmware endpoint not provided")
	}
	client, err := dialVMware(ctx, endpoint, metadata)
	if err != nil {
		return err
	}
	_ = client.Logout(ctx)
	return nil
}

// RegisterLB + DeregisterLB stay as HTTP forwards. NSX-T load-balancer wiring
// is substantial enough to live in a dedicated backend service.
func (v *vmwareAdapter) RegisterLB(ctx context.Context, nodeID string, clusterMeta map[string]any) error {
	return v.httpAdapter.RegisterLB(ctx, nodeID, clusterMeta)
}
func (v *vmwareAdapter) DeregisterLB(ctx context.Context, nodeID string, clusterMeta map[string]any) error {
	return v.httpAdapter.DeregisterLB(ctx, nodeID, clusterMeta)
}

func pickVMwareEndpoint(metadata map[string]string) string {
	if v := strings.TrimSpace(metadata["_endpoint_url"]); v != "" {
		return v
	}
	if v := strings.TrimSpace(metadata["url"]); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("VMWARE_URL")); v != "" {
		return v
	}
	return ""
}

func dialVMware(ctx context.Context, endpoint string, metadata map[string]string) (*govmomi.Client, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("parse vmware url: %w", err)
	}
	username := firstNonEmpty(metadata["_cred_username"], os.Getenv("VMWARE_USERNAME"))
	password := firstNonEmpty(metadata["_cred_password"], os.Getenv("VMWARE_PASSWORD"))
	if username == "" || password == "" {
		return nil, errors.New("vmware credentials missing (username/password)")
	}
	parsed.User = url.UserPassword(username, password)

	insecure := strings.EqualFold(firstNonEmpty(metadata["_cred_insecure"], os.Getenv("VMWARE_INSECURE")), "true")
	client, err := govmomi.NewClient(ctx, parsed, insecure)
	if err != nil {
		return nil, fmt.Errorf("govmomi client: %w", err)
	}
	return client, nil
}

func vmwareApply(ctx context.Context, log *zap.Logger, endpoint, nodeID string, opts Options, metadata map[string]string) (*ApplyResult, error) {
	client, err := dialVMware(ctx, endpoint, metadata)
	if err != nil {
		return nil, err
	}
	defer func() { _ = client.Logout(ctx) }()

	finder := find.NewFinder(client.Client, true)

	dcName := metadata["datacenter"]
	if dcName == "" {
		return nil, errors.New("metadata.datacenter required")
	}
	dc, err := finder.Datacenter(ctx, dcName)
	if err != nil {
		return nil, fmt.Errorf("find datacenter %q: %w", dcName, err)
	}
	finder.SetDatacenter(dc)

	templateName := firstNonEmpty(metadata["template"], opts.Template)
	if templateName == "" {
		return nil, errors.New("metadata.template (VM template path) required")
	}
	tmpl, err := finder.VirtualMachine(ctx, templateName)
	if err != nil {
		return nil, fmt.Errorf("find template %q: %w", templateName, err)
	}

	targetFolder := metadata["folder"]
	folder, err := finder.FolderOrDefault(ctx, targetFolder)
	if err != nil {
		return nil, fmt.Errorf("find folder %q: %w", targetFolder, err)
	}

	clusterName := metadata["cluster"]
	pool, err := finder.ResourcePool(ctx, fmt.Sprintf("%s/Resources", clusterName))
	if err != nil {
		return nil, fmt.Errorf("find resource pool for cluster %q: %w", clusterName, err)
	}

	dsName := metadata["datastore"]
	ds, err := finder.Datastore(ctx, dsName)
	if err != nil {
		return nil, fmt.Errorf("find datastore %q: %w", dsName, err)
	}
	dsRef := ds.Reference()
	poolRef := pool.Reference()

	cloneSpec := &types.VirtualMachineCloneSpec{
		Location: types.VirtualMachineRelocateSpec{
			Pool:      &poolRef,
			Datastore: &dsRef,
		},
		PowerOn:  true,
		Template: false,
		Config: &types.VirtualMachineConfigSpec{
			ExtraConfig: buildVMwareExtraConfig(metadata),
		},
	}

	task, err := tmpl.Clone(ctx, folder, vmwareCloneName(nodeID), *cloneSpec)
	if err != nil {
		return nil, fmt.Errorf("clone template: %w", err)
	}
	if err := task.Wait(ctx); err != nil {
		return nil, fmt.Errorf("clone task wait: %w", err)
	}

	log.Info("vmware clone ok",
		zap.String("node_id", nodeID),
		zap.String("template", templateName),
		zap.String("datacenter", dcName))
	return &ApplyResult{OperationID: fmt.Sprintf("vmware-%s", nodeID)}, nil
}

func vmwareDestroy(ctx context.Context, log *zap.Logger, endpoint, nodeID string) error {
	client, err := dialVMware(ctx, endpoint, nil)
	if err != nil {
		return err
	}
	defer func() { _ = client.Logout(ctx) }()
	finder := find.NewFinder(client.Client, true)

	vm, err := finder.VirtualMachine(ctx, vmwareCloneName(nodeID))
	if err != nil {
		var notFound *find.NotFoundError
		if errors.As(err, &notFound) {
			log.Warn("vmware vm not found; skipping destroy", zap.String("node_id", nodeID))
			return nil
		}
		return fmt.Errorf("find vm: %w", err)
	}
	if err := powerOffVM(ctx, vm); err != nil {
		log.Warn("power off vm failed; attempting destroy anyway", zap.Error(err))
	}
	task, err := vm.Destroy(ctx)
	if err != nil {
		return fmt.Errorf("destroy vm: %w", err)
	}
	return task.Wait(ctx)
}

func powerOffVM(ctx context.Context, vm *object.VirtualMachine) error {
	state, err := vm.PowerState(ctx)
	if err != nil {
		return err
	}
	if state == types.VirtualMachinePowerStatePoweredOff {
		return nil
	}
	task, err := vm.PowerOff(ctx)
	if err != nil {
		return err
	}
	return task.Wait(ctx)
}

// buildVMwareExtraConfig translates cloud-init user-data + network config into
// VMware guestinfo custom properties. cloud-init's VMware datasource reads
// keys prefixed with guestinfo.metadata / guestinfo.userdata (base64+gzip is
// also supported by newer cloud-init; plain base64 is the broadest path).
func buildVMwareExtraConfig(metadata map[string]string) []types.BaseOptionValue {
	userData := metadata["user_data"]
	if userData == "" && metadata["install_one_liner"] != "" {
		userData = "#cloud-config\nruncmd:\n  - [ sh, -c, \"" + escapeYAMLScalar(metadata["install_one_liner"]) + "\" ]\n"
	}
	metaData := fmt.Sprintf("instance-id: %s\nlocal-hostname: %s\n",
		uuidOr(metadata["instance_id"]),
		strings.TrimSpace(metadata["hostname"]))

	out := []types.BaseOptionValue{
		&types.OptionValue{Key: "guestinfo.metadata", Value: base64.StdEncoding.EncodeToString([]byte(metaData))},
		&types.OptionValue{Key: "guestinfo.metadata.encoding", Value: "base64"},
	}
	if userData != "" {
		out = append(out,
			&types.OptionValue{Key: "guestinfo.userdata", Value: base64.StdEncoding.EncodeToString([]byte(userData))},
			&types.OptionValue{Key: "guestinfo.userdata.encoding", Value: "base64"},
		)
	}
	return out
}

func vmwareCloneName(nodeID string) string {
	return "controlone-" + nodeID
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

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}
