package fleet

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

const (
	defaultParallel       = 5
	defaultCommandTimeout = 5 * time.Minute
)

// Target represents a single host to provision.
type Target struct {
	Host string `json:"host"`
	Port int    `json:"port,omitempty"`
	User string `json:"user,omitempty"`
}

// ProvisionRequest captures all parameters for a fleet enrollment run.
type ProvisionRequest struct {
	Targets     []Target
	SSHUser     string
	SSHPort     int
	SSHKey      []byte
	SSHPassword string
	TokenRaw    string
	CPURL       string
	Parallel    int
	Labels      map[string]string
}

// ProvisionResult captures the outcome of provisioning a single target.
type ProvisionResult struct {
	Host       string
	Port       int
	Success    bool
	Output     string
	Error      string
	DurationMs int
}

// Provisioner orchestrates parallel SSH-based agent installation across targets.
type Provisioner struct {
	log       *zap.Logger
	sshClient *SSHClient
}

// NewProvisioner creates a new fleet provisioner.
func NewProvisioner(log *zap.Logger) *Provisioner {
	return &Provisioner{
		log:       log,
		sshClient: NewSSHClient(log),
	}
}

// Provision installs the agent on all targets concurrently and returns results.
func (p *Provisioner) Provision(ctx context.Context, req ProvisionRequest) []ProvisionResult {
	parallel := req.Parallel
	if parallel <= 0 {
		parallel = defaultParallel
	}

	results := make([]ProvisionResult, len(req.Targets))
	sem := make(chan struct{}, parallel)
	var wg sync.WaitGroup

	for i, target := range req.Targets {
		// Resolve per-target overrides
		host := target.Host
		port := target.Port
		if port == 0 {
			port = req.SSHPort
		}
		if port == 0 {
			port = 22
		}
		user := target.User
		if user == "" {
			user = req.SSHUser
		}

		wg.Add(1)
		go func(idx int, host string, port int, user string) {
			defer wg.Done()

			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				results[idx] = ProvisionResult{
					Host:  host,
					Port:  port,
					Error: "context cancelled before execution",
				}
				return
			}

			results[idx] = p.provisionOne(ctx, host, port, user, req)
		}(i, host, port, user)
	}

	wg.Wait()
	return results
}

func (p *Provisioner) provisionOne(ctx context.Context, host string, port int, user string, req ProvisionRequest) ProvisionResult {
	start := time.Now()
	result := ProvisionResult{
		Host: host,
		Port: port,
	}

	sshCfg := SSHConfig{
		Host:     host,
		Port:     port,
		User:     user,
		Password: req.SSHPassword,
		Key:      req.SSHKey,
	}

	client, err := p.sshClient.Connect(sshCfg)
	if err != nil {
		result.Error = fmt.Sprintf("ssh connect: %v", err)
		result.DurationMs = int(time.Since(start).Milliseconds())
		p.log.Warn("fleet provision ssh connect failed",
			zap.String("host", host),
			zap.Int("port", port),
			zap.Error(err),
		)
		return result
	}
	defer client.Close()

	installCmd := fmt.Sprintf(
		"curl -fsSL '%s/api/v1/agent/install-script?token=%s' | bash",
		req.CPURL, req.TokenRaw,
	)

	p.log.Info("running install command on target",
		zap.String("host", host),
		zap.Int("port", port),
	)

	stdout, stderr, exitCode, err := p.sshClient.RunCommand(client, installCmd, defaultCommandTimeout)
	result.DurationMs = int(time.Since(start).Milliseconds())

	output := stdout
	if stderr != "" {
		output += "\n--- STDERR ---\n" + stderr
	}
	result.Output = output

	if err != nil || exitCode != 0 {
		errMsg := ""
		if err != nil {
			errMsg = err.Error()
		}
		if exitCode != 0 {
			errMsg = fmt.Sprintf("exit code %d: %s", exitCode, errMsg)
		}
		result.Error = errMsg
		p.log.Warn("fleet provision failed on target",
			zap.String("host", host),
			zap.Int("exit_code", exitCode),
			zap.Error(err),
		)
		return result
	}

	result.Success = true
	p.log.Info("fleet provision succeeded on target",
		zap.String("host", host),
		zap.Int("duration_ms", result.DurationMs),
	)
	return result
}
