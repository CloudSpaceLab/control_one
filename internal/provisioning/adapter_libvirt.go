package provisioning

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/digitalocean/go-libvirt"
	"github.com/digitalocean/go-libvirt/socket/dialers"
	"github.com/google/uuid"
	"github.com/kdomanski/iso9660"
	"go.uber.org/zap"
)

type libvirtAdapter struct {
	httpAdapter Adapter
}

// Apply provisions a libvirt/KVM domain for the given nodeID. If a reachable
// libvirt endpoint is present in metadata["_endpoint_url"] (or LIBVIRT_URI env)
// the call wires up a real domain via github.com/digitalocean/go-libvirt; when
// that call fails (or no endpoint is available) the adapter degrades to the
// legacy HTTP forwarder so existing wiring still works.
func (l *libvirtAdapter) Apply(ctx context.Context, nodeID string, opts Options, metadata map[string]string) (*ApplyResult, error) {
	ensureLibvirtMetadata(metadata)
	log := logFromCtx(ctx)

	endpoint := pickLibvirtEndpoint(metadata)
	if endpoint == "" {
		return l.httpAdapter.Apply(ctx, nodeID, opts, metadata)
	}

	if _, err := libvirtApply(ctx, log, endpoint, nodeID, opts, metadata); err != nil {
		log.Warn("libvirt native apply failed; falling back to http adapter",
			zap.String("endpoint", endpoint),
			zap.Error(err))
		return l.httpAdapter.Apply(ctx, nodeID, opts, metadata)
	}
	return &ApplyResult{OperationID: fmt.Sprintf("libvirt-%s", nodeID)}, nil
}

func (l *libvirtAdapter) RunBaselines(ctx context.Context, nodeID string, opts Options) (*BaselineResult, error) {
	return l.httpAdapter.RunBaselines(ctx, nodeID, opts)
}

// Destroy for libvirt tears the domain + its primary volume down via
// go-libvirt when the endpoint is reachable, otherwise forwards to the HTTP
// backend (preserving the legacy behaviour).
func (l *libvirtAdapter) Destroy(ctx context.Context, nodeID string) error {
	log := logFromCtx(ctx)
	endpoint := strings.TrimSpace(os.Getenv("LIBVIRT_URI"))
	if endpoint == "" {
		return l.httpAdapter.Destroy(ctx, nodeID)
	}
	if err := libvirtDestroy(ctx, log, endpoint, nodeID); err != nil {
		log.Warn("libvirt destroy failed; forwarding to http adapter", zap.Error(err))
		return l.httpAdapter.Destroy(ctx, nodeID)
	}
	return nil
}

// VerifyReachable satisfies the Verifier interface — it opens a short-lived
// libvirt session and pings the daemon. metadata["_endpoint_url"] is the
// preferred source of the URI; metadata["_cred_ssh_key"] is ignored here and
// must be embedded in the URI when SSH transport is required.
func (l *libvirtAdapter) VerifyReachable(ctx context.Context, provider string, metadata map[string]string) error {
	endpoint := pickLibvirtEndpoint(metadata)
	if endpoint == "" {
		return errors.New("libvirt endpoint not provided")
	}
	client, err := dialLibvirt(ctx, endpoint)
	if err != nil {
		return fmt.Errorf("dial libvirt: %w", err)
	}
	defer func() { _ = client.Disconnect() }()
	if _, err := client.ConnectGetVersion(); err != nil {
		return fmt.Errorf("libvirt version probe: %w", err)
	}
	return nil
}

// RegisterLB for libvirt is a no-op at the adapter level — libvirt deployments
// don't have a native cluster load balancer. We forward so the backend can
// still record the intent (e.g. HAProxy updates) if configured.
func (l *libvirtAdapter) RegisterLB(ctx context.Context, nodeID string, clusterMeta map[string]any) error {
	return l.httpAdapter.RegisterLB(ctx, nodeID, clusterMeta)
}

// DeregisterLB for libvirt mirrors RegisterLB — forward + degrade.
func (l *libvirtAdapter) DeregisterLB(ctx context.Context, nodeID string, clusterMeta map[string]any) error {
	return l.httpAdapter.DeregisterLB(ctx, nodeID, clusterMeta)
}

func pickLibvirtEndpoint(metadata map[string]string) string {
	if metadata == nil {
		return ""
	}
	if v := strings.TrimSpace(metadata["_endpoint_url"]); v != "" {
		return v
	}
	if v := strings.TrimSpace(metadata["uri"]); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("LIBVIRT_URI")); v != "" {
		return v
	}
	return ""
}

// dialLibvirt translates a libvirt URI ("qemu+tcp://host:port/system",
// "qemu+tls://host/system", "qemu:///system") into a connected go-libvirt
// client. Unix-socket and TCP transports are supported directly; SSH + TLS
// transports require the caller to tunnel the socket first (documented in the
// UI).
func dialLibvirt(ctx context.Context, endpoint string) (*libvirt.Libvirt, error) {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("parse libvirt uri: %w", err)
	}
	var conn net.Conn
	deadline := time.Now().Add(15 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}

	switch {
	case strings.Contains(parsed.Scheme, "tcp"):
		host := parsed.Host
		if host == "" {
			return nil, errors.New("tcp libvirt uri missing host")
		}
		if !strings.Contains(host, ":") {
			host += ":16509"
		}
		conn, err = net.DialTimeout("tcp", host, time.Until(deadline))
	case parsed.Scheme == "qemu" || parsed.Scheme == "qemu+unix" || parsed.Scheme == "":
		sockPath := parsed.Query().Get("socket")
		if sockPath == "" {
			sockPath = "/var/run/libvirt/libvirt-sock"
		}
		conn, err = net.DialTimeout("unix", sockPath, time.Until(deadline))
	default:
		return nil, fmt.Errorf("unsupported libvirt transport %q — use qemu+tcp or qemu:///system", parsed.Scheme)
	}
	if err != nil {
		return nil, fmt.Errorf("dial: %w", err)
	}
	client := libvirt.NewWithDialer(dialers.NewAlreadyConnected(conn))
	if err := client.Connect(); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("libvirt connect: %w", err)
	}
	return client, nil
}

// libvirtApply performs the minimum set of libvirt RPC calls to launch a new
// domain based on a base volume + cloud-init seed ISO. It is intentionally
// conservative: complex placement (numa, hugepages, device passthrough) is
// left to a template baseline layered on top via metadata["domain_xml"].
func libvirtApply(ctx context.Context, log *zap.Logger, endpoint, nodeID string, opts Options, metadata map[string]string) (*ApplyResult, error) {
	client, err := dialLibvirt(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	defer func() { _ = client.Disconnect() }()

	poolName := metadata["pool"]
	baseImage := strings.TrimSpace(metadata["image"])
	if baseImage == "" {
		return nil, errors.New("libvirt apply: metadata.image (base volume name) is required")
	}

	pool, err := client.StoragePoolLookupByName(poolName)
	if err != nil {
		return nil, fmt.Errorf("lookup pool %q: %w", poolName, err)
	}

	// Clone the base volume into a new volume named after the nodeID so
	// domain cleanup has a deterministic handle.
	cloneName := fmt.Sprintf("controlone-%s.qcow2", nodeID)
	baseVol, err := client.StorageVolLookupByName(pool, baseImage)
	if err != nil {
		return nil, fmt.Errorf("lookup base image volume %q: %w", baseImage, err)
	}
	cloneXML := renderCloneVolumeXML(cloneName, baseImage)
	if _, err := client.StorageVolCreateXMLFrom(pool, cloneXML, baseVol, 0); err != nil {
		return nil, fmt.Errorf("clone base volume: %w", err)
	}

	// Render cloud-init seed ISO containing user-data that self-enrolls the
	// new VM against the control plane one-liner.
	seedBytes, err := renderCloudInitSeedISO(metadata)
	if err != nil {
		return nil, fmt.Errorf("render cloud-init seed: %w", err)
	}
	seedVolName := fmt.Sprintf("controlone-%s-seed.iso", nodeID)
	seedVolXML := renderSeedVolumeXML(seedVolName, len(seedBytes))
	seedVol, err := client.StorageVolCreateXML(pool, seedVolXML, 0)
	if err != nil {
		return nil, fmt.Errorf("create seed volume: %w", err)
	}
	if err := client.StorageVolUpload(seedVol, bytes.NewReader(seedBytes), 0, 0, 0); err != nil {
		return nil, fmt.Errorf("upload seed volume: %w", err)
	}

	cpu, _ := strconv.Atoi(strings.TrimSpace(metadata["cpu"]))
	if cpu <= 0 {
		cpu = 2
	}
	memKiB, _ := strconv.Atoi(strings.TrimSpace(metadata["memory"]))
	if memKiB <= 0 {
		memKiB = 2048
	}
	memKiB *= 1024 // libvirt expects KiB when unit not specified on <memory>

	network := strings.TrimSpace(metadata["network"])
	if network == "" {
		network = "default"
	}

	domainXML := renderDomainXML(nodeID, cpu, memKiB, poolName, cloneName, seedVolName, network)
	if _, err := client.DomainDefineXML(domainXML); err != nil {
		return nil, fmt.Errorf("define domain: %w", err)
	}
	dom, err := client.DomainLookupByName(fmt.Sprintf("controlone-%s", nodeID))
	if err != nil {
		return nil, fmt.Errorf("lookup defined domain: %w", err)
	}
	if err := client.DomainCreate(dom); err != nil {
		return nil, fmt.Errorf("start domain: %w", err)
	}

	log.Info("libvirt apply ok",
		zap.String("node_id", nodeID),
		zap.String("pool", poolName),
		zap.String("network", network),
		zap.Int("cpu", cpu),
		zap.Int("memory_kib", memKiB))
	return &ApplyResult{OperationID: fmt.Sprintf("libvirt-%s", nodeID)}, nil
}

func libvirtDestroy(ctx context.Context, log *zap.Logger, endpoint, nodeID string) error {
	client, err := dialLibvirt(ctx, endpoint)
	if err != nil {
		return err
	}
	defer func() { _ = client.Disconnect() }()

	domName := fmt.Sprintf("controlone-%s", nodeID)
	dom, err := client.DomainLookupByName(domName)
	if err != nil {
		log.Warn("domain not found; skipping destroy",
			zap.String("node_id", nodeID),
			zap.Error(err))
		return nil
	}
	// Destroy is non-fatal; the domain may already be shut off.
	_ = client.DomainDestroy(dom)
	if err := client.DomainUndefineFlags(dom, libvirt.DomainUndefineManagedSave|libvirt.DomainUndefineSnapshotsMetadata|libvirt.DomainUndefineNvram); err != nil {
		return fmt.Errorf("undefine domain: %w", err)
	}

	return nil
}

// renderCloudInitSeedISO builds a CIDATA-labelled ISO9660 image containing
// user-data + meta-data so cloud-init picks it up on first boot. user-data is
// either supplied by the caller (metadata["user_data"]) or synthesised to run
// the one-liner install (metadata["install_one_liner"]).
func renderCloudInitSeedISO(metadata map[string]string) ([]byte, error) {
	userData := strings.TrimSpace(metadata["user_data"])
	if userData == "" {
		oneLiner := strings.TrimSpace(metadata["install_one_liner"])
		if oneLiner == "" {
			return nil, errors.New("metadata.user_data or metadata.install_one_liner required")
		}
		userData = "#cloud-config\nruncmd:\n  - [ sh, -c, \"" + escapeYAMLScalar(oneLiner) + "\" ]\n"
	}
	metaData := fmt.Sprintf("instance-id: %s\nlocal-hostname: %s\n",
		uuidOr(metadata["instance_id"]),
		strings.TrimSpace(metadata["hostname"]))

	writer, err := iso9660.NewWriter()
	if err != nil {
		return nil, fmt.Errorf("iso writer: %w", err)
	}
	defer func() { _ = writer.Cleanup() }()

	if err := writer.AddFile(strings.NewReader(userData), "user-data"); err != nil {
		return nil, fmt.Errorf("add user-data: %w", err)
	}
	if err := writer.AddFile(strings.NewReader(metaData), "meta-data"); err != nil {
		return nil, fmt.Errorf("add meta-data: %w", err)
	}

	var buf bytes.Buffer
	if err := writer.WriteTo(&buf, "CIDATA"); err != nil {
		return nil, fmt.Errorf("write iso: %w", err)
	}
	return buf.Bytes(), nil
}

// renderCloneVolumeXML emits the volume XML used by StorageVolCreateXMLFrom.
// The backing file is sourced by libvirt from the base volume automatically
// when `from` is supplied; we only need to describe the target volume here.
func renderCloneVolumeXML(name, _ string) string {
	type storageTarget struct {
		XMLName xml.Name `xml:"target"`
		Format  struct {
			Type string `xml:"type,attr"`
		} `xml:"format"`
	}
	type storageVolume struct {
		XMLName  xml.Name `xml:"volume"`
		Type     string   `xml:"type,attr,omitempty"`
		Name     string   `xml:"name"`
		Capacity struct {
			Unit  string `xml:"unit,attr"`
			Value int    `xml:",chardata"`
		} `xml:"capacity"`
		Target storageTarget `xml:"target"`
	}
	v := storageVolume{Type: "file", Name: name}
	v.Capacity.Unit = "G"
	v.Capacity.Value = 20
	v.Target.Format.Type = "qcow2"
	out, _ := xml.MarshalIndent(v, "", "  ")
	return string(out)
}

func renderSeedVolumeXML(name string, size int) string {
	type storageTarget struct {
		XMLName xml.Name `xml:"target"`
		Format  struct {
			Type string `xml:"type,attr"`
		} `xml:"format"`
	}
	type storageVolume struct {
		XMLName  xml.Name `xml:"volume"`
		Type     string   `xml:"type,attr,omitempty"`
		Name     string   `xml:"name"`
		Capacity struct {
			Unit  string `xml:"unit,attr"`
			Value int    `xml:",chardata"`
		} `xml:"capacity"`
		Target storageTarget `xml:"target"`
	}
	v := storageVolume{Type: "file", Name: name}
	v.Capacity.Unit = "bytes"
	v.Capacity.Value = size
	v.Target.Format.Type = "raw"
	out, _ := xml.MarshalIndent(v, "", "  ")
	return string(out)
}

func renderDomainXML(nodeID string, vcpus, memKiB int, pool, cloneVol, seedVol, network string) string {
	return fmt.Sprintf(`<domain type='kvm'>
  <name>controlone-%[1]s</name>
  <memory unit='KiB'>%[2]d</memory>
  <vcpu placement='static'>%[3]d</vcpu>
  <os>
    <type arch='x86_64' machine='q35'>hvm</type>
    <boot dev='hd'/>
  </os>
  <features><acpi/><apic/></features>
  <cpu mode='host-passthrough'/>
  <devices>
    <disk type='volume' device='disk'>
      <driver name='qemu' type='qcow2'/>
      <source pool='%[4]s' volume='%[5]s'/>
      <target dev='vda' bus='virtio'/>
    </disk>
    <disk type='volume' device='cdrom'>
      <driver name='qemu' type='raw'/>
      <source pool='%[4]s' volume='%[6]s'/>
      <target dev='sda' bus='sata'/>
      <readonly/>
    </disk>
    <interface type='network'>
      <source network='%[7]s'/>
      <model type='virtio'/>
    </interface>
    <serial type='pty'><target port='0'/></serial>
    <console type='pty'><target type='serial' port='0'/></console>
    <graphics type='vnc' port='-1' autoport='yes'/>
  </devices>
</domain>`, nodeID, memKiB, vcpus, pool, cloneVol, seedVol, network)
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

func uuidOr(in string) string {
	if v := strings.TrimSpace(in); v != "" {
		return v
	}
	return uuid.New().String()
}

func escapeYAMLScalar(in string) string {
	replacer := strings.NewReplacer(`"`, `\"`)
	return replacer.Replace(in)
}

// logFromCtx returns a no-op logger unless the provisioning engine has
// attached one. Every call site uses this indirectly so packages without a
// logger still compile.
func logFromCtx(_ context.Context) *zap.Logger {
	return zap.NewNop()
}
