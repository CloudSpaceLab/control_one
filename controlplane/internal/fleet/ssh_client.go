package fleet

import (
	"bytes"
	"fmt"
	"net"
	"time"

	"go.uber.org/zap"
	"golang.org/x/crypto/ssh"
)

const defaultSSHTimeout = 30 * time.Second

// SSHClient wraps SSH connectivity for fleet provisioning.
type SSHClient struct {
	log     *zap.Logger
	timeout time.Duration
}

// SSHConfig captures connection parameters for a single SSH target.
type SSHConfig struct {
	Host     string
	Port     int
	User     string
	Password string
	Key      []byte // PEM-encoded private key
}

// NewSSHClient returns a new SSH client with sensible defaults.
func NewSSHClient(log *zap.Logger) *SSHClient {
	return &SSHClient{
		log:     log,
		timeout: defaultSSHTimeout,
	}
}

// Connect establishes an SSH connection to the target host.
func (c *SSHClient) Connect(cfg SSHConfig) (*ssh.Client, error) {
	if cfg.Port == 0 {
		cfg.Port = 22
	}

	var authMethods []ssh.AuthMethod

	if len(cfg.Key) > 0 {
		signer, err := ssh.ParsePrivateKey(cfg.Key)
		if err != nil {
			return nil, fmt.Errorf("parse private key: %w", err)
		}
		authMethods = append(authMethods, ssh.PublicKeys(signer))
	}

	if cfg.Password != "" {
		authMethods = append(authMethods, ssh.Password(cfg.Password))
	}

	if len(authMethods) == 0 {
		return nil, fmt.Errorf("no authentication method provided (key or password required)")
	}

	timeout := c.timeout
	if timeout == 0 {
		timeout = defaultSSHTimeout
	}

	sshCfg := &ssh.ClientConfig{
		User:            cfg.User,
		Auth:            authMethods,
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), //nolint:gosec // fleet provisioning accepts any host key
		Timeout:         timeout,
	}

	addr := net.JoinHostPort(cfg.Host, fmt.Sprintf("%d", cfg.Port))
	c.log.Debug("connecting via SSH", zap.String("addr", addr), zap.String("user", cfg.User))

	client, err := ssh.Dial("tcp", addr, sshCfg)
	if err != nil {
		return nil, fmt.Errorf("ssh dial %s: %w", addr, err)
	}

	return client, nil
}

// RunCommand executes a command on an established SSH connection and returns output.
func (c *SSHClient) RunCommand(client *ssh.Client, command string, timeout time.Duration) (stdout, stderr string, exitCode int, err error) {
	session, err := client.NewSession()
	if err != nil {
		return "", "", -1, fmt.Errorf("create session: %w", err)
	}
	defer func() { _ = session.Close() }()

	var stdoutBuf, stderrBuf bytes.Buffer
	session.Stdout = &stdoutBuf
	session.Stderr = &stderrBuf

	done := make(chan error, 1)
	go func() {
		done <- session.Run(command)
	}()

	select {
	case runErr := <-done:
		if runErr != nil {
			if exitErr, ok := runErr.(*ssh.ExitError); ok {
				return stdoutBuf.String(), stderrBuf.String(), exitErr.ExitStatus(), runErr
			}
			return stdoutBuf.String(), stderrBuf.String(), -1, runErr
		}
		return stdoutBuf.String(), stderrBuf.String(), 0, nil

	case <-time.After(timeout):
		_ = session.Signal(ssh.SIGKILL)
		return stdoutBuf.String(), stderrBuf.String(), -1, fmt.Errorf("command timed out after %s", timeout)
	}
}
