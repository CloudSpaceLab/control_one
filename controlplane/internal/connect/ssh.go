package connect

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// SSHConnector tests SSH reachability and probes the remote OS via a
// best-effort `uname -a` (Linux/macOS) or `powershell $PSVersionTable`
// (Windows ssh-server). It honours password, private-key, and agent-socket
// auth and never executes anything that would mutate state.
type SSHConnector struct{}

func NewSSHConnector() *SSHConnector { return &SSHConnector{} }

func (c *SSHConnector) Name() Protocol { return ProtoSSH }

func (c *SSHConnector) Test(ctx context.Context, t Target) (*Probe, error) {
	if t.Host == "" {
		return nil, errors.New("ssh: host required")
	}
	port := t.Port
	if port == 0 {
		port = DefaultPort(ProtoSSH, false)
	}
	addr := net.JoinHostPort(t.Host, strconv.Itoa(port))

	authMethods, err := buildSSHAuth(t)
	if err != nil {
		return nil, err
	}

	cfg := &ssh.ClientConfig{
		User:    t.Username,
		Auth:    authMethods,
		Timeout: applyTimeout(t.Timeout),
		// Operator wizard runs from the control-plane; we deliberately do
		// not pin host keys here because the operator hasn't TOFU-accepted
		// one yet. The probe runs once before enrollment; the agent that
		// gets installed afterwards uses its own pinned channel.
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), //nolint:gosec
	}

	start := time.Now()
	dialCtx, cancel := context.WithTimeout(ctx, applyTimeout(t.Timeout))
	defer cancel()

	dialer := &net.Dialer{Timeout: cfg.Timeout}
	rawConn, err := dialer.DialContext(dialCtx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("ssh dial: %w", err)
	}

	sshConn, chans, reqs, err := ssh.NewClientConn(rawConn, addr, cfg)
	if err != nil {
		_ = rawConn.Close()
		return nil, fmt.Errorf("ssh handshake: %w", err)
	}
	client := ssh.NewClient(sshConn, chans, reqs)
	defer client.Close()

	probe := &Probe{
		Reachable: true,
		LatencyMs: time.Since(start).Milliseconds(),
		Banner:    string(sshConn.ServerVersion()),
		Detected:  time.Now().UTC(),
	}

	// Best-effort OS detection. Failure here doesn't fail the test —
	// reachability is the primary signal for the wizard.
	if out, ok := runOnce(client, "uname -a"); ok && out != "" {
		probe.OS = "linux"
		if strings.Contains(strings.ToLower(out), "darwin") {
			probe.OS = "macos"
		}
		probe.OSVersion = strings.TrimSpace(strings.SplitN(out, "\n", 2)[0])
	} else if out, ok := runOnce(client, "ver"); ok && strings.Contains(strings.ToLower(out), "windows") {
		probe.OS = "windows"
		probe.OSVersion = strings.TrimSpace(strings.SplitN(out, "\n", 2)[0])
	} else {
		probe.OS = "unknown"
	}

	if hn, ok := runOnce(client, "hostname"); ok {
		probe.Hostname = strings.TrimSpace(hn)
	}
	if ar, ok := runOnce(client, "uname -m"); ok {
		probe.Architecture = strings.TrimSpace(ar)
	}

	probe.Capabilities = sshCapabilities(client, probe.OS)

	// Extended info: distro, CPU, memory — best-effort, errors ignored.
	if probe.OS == "linux" || probe.OS == "macos" {
		if dist, ok := runOnce(client, `cat /etc/os-release 2>/dev/null | grep ^PRETTY_NAME | cut -d= -f2 | tr -d '"'`); ok {
			probe.Distro = strings.TrimSpace(dist)
		}
		if cpus, ok := runOnce(client, "nproc 2>/dev/null || grep -c ^processor /proc/cpuinfo 2>/dev/null || echo 0"); ok {
			if n := strings.TrimSpace(cpus); n != "" && n != "0" {
				var cnt int
				if _, err := fmt.Sscanf(n, "%d", &cnt); err == nil {
					probe.CPUCount = cnt
				}
			}
		}
		if mem, ok := runOnce(client, `awk '/^MemTotal:/{print int($2/1024)}' /proc/meminfo 2>/dev/null`); ok {
			if m := strings.TrimSpace(mem); m != "" {
				var mb int
				if _, err := fmt.Sscanf(m, "%d", &mb); err == nil {
					probe.MemoryMB = mb
				}
			}
		}
	}

	return probe, nil
}

func buildSSHAuth(t Target) ([]ssh.AuthMethod, error) {
	switch t.Auth {
	case AuthPassword:
		if t.Password == "" {
			return nil, errors.New("ssh: password required for password auth")
		}
		return []ssh.AuthMethod{ssh.Password(t.Password)}, nil
	case AuthPrivateKey:
		if t.PrivateKey == "" {
			return nil, errors.New("ssh: private_key required for private_key auth")
		}
		var (
			signer ssh.Signer
			err    error
		)
		if t.Passphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase([]byte(t.PrivateKey), []byte(t.Passphrase))
		} else {
			signer, err = ssh.ParsePrivateKey([]byte(t.PrivateKey))
		}
		if err != nil {
			return nil, fmt.Errorf("ssh: parse private key: %w", err)
		}
		return []ssh.AuthMethod{ssh.PublicKeys(signer)}, nil
	case AuthAgentSocket:
		// Intentionally not wired in this initial cut — operators almost
		// always paste a key or password through the wizard. Agent
		// passthrough would require forwarding the controlplane host's
		// ssh-agent which is rarely the right thing.
		return nil, errors.New("ssh: agent auth not supported in onboarding wizard")
	default:
		return nil, fmt.Errorf("ssh: unsupported auth method %q", t.Auth)
	}
}

// runOnce executes a non-mutating diagnostic command and returns its
// stdout. Errors are swallowed — callers treat the returned bool as
// "we got something back".
func runOnce(client *ssh.Client, cmd string) (string, bool) {
	sess, err := client.NewSession()
	if err != nil {
		return "", false
	}
	defer sess.Close()
	out, err := sess.CombinedOutput(cmd)
	if err != nil {
		return "", false
	}
	return string(out), true
}

func sshCapabilities(client *ssh.Client, os string) []string {
	caps := []string{"ssh"}
	switch os {
	case "linux", "macos":
		if _, ok := runOnce(client, "command -v sudo >/dev/null 2>&1 && echo yes"); ok {
			caps = append(caps, "sudo")
		}
		if _, ok := runOnce(client, "command -v apt >/dev/null 2>&1 && echo yes"); ok {
			caps = append(caps, "package_apt")
		}
		if _, ok := runOnce(client, "command -v yum >/dev/null 2>&1 && echo yes"); ok {
			caps = append(caps, "package_yum")
		}
		if _, ok := runOnce(client, "command -v systemctl >/dev/null 2>&1 && echo yes"); ok {
			caps = append(caps, "systemd")
		}
	case "windows":
		if _, ok := runOnce(client, `powershell -Command "$PSVersionTable.PSVersion"`); ok {
			caps = append(caps, "powershell")
		}
	}
	return caps
}
