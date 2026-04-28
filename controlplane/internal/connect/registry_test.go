package connect

import (
	"context"
	"errors"
	"testing"
	"time"
)

type stubConnector struct {
	proto Protocol
	probe *Probe
	err   error
}

func (s *stubConnector) Name() Protocol { return s.proto }
func (s *stubConnector) Test(_ context.Context, _ Target) (*Probe, error) {
	return s.probe, s.err
}

func TestRegistryRoutesByProtocol(t *testing.T) {
	r := &Registry{byProto: map[Protocol]Connector{}}
	sshProbe := &Probe{Reachable: true, OS: "linux", Detected: time.Now().UTC()}
	r.Register(&stubConnector{proto: ProtoSSH, probe: sshProbe})
	r.Register(&stubConnector{proto: ProtoWinRM, probe: &Probe{Reachable: true, OS: "windows"}})

	got, err := r.Test(context.Background(), Target{Protocol: ProtoSSH, Host: "h"})
	if err != nil {
		t.Fatalf("ssh: %v", err)
	}
	if got != sshProbe {
		t.Errorf("expected ssh stub probe, got %+v", got)
	}

	got2, err := r.Test(context.Background(), Target{Protocol: ProtoWinRM, Host: "h"})
	if err != nil || got2.OS != "windows" {
		t.Errorf("winrm: probe=%+v err=%v", got2, err)
	}
}

func TestRegistryRejectsUnknownProtocol(t *testing.T) {
	r := &Registry{byProto: map[Protocol]Connector{}}
	if _, err := r.Test(context.Background(), Target{Protocol: "telepathy"}); err == nil {
		t.Fatal("expected unknown-protocol error")
	}
}

func TestNewRegistryIncludesAllProtocols(t *testing.T) {
	r := NewRegistry()
	supported := map[Protocol]bool{}
	for _, p := range r.Supported() {
		supported[p] = true
	}
	for _, want := range []Protocol{ProtoSSH, ProtoWinRM, ProtoRDP} {
		if !supported[want] {
			t.Errorf("missing built-in connector: %s", want)
		}
	}
}

func TestDefaultPortMatchesSpec(t *testing.T) {
	cases := []struct {
		p     Protocol
		https bool
		want  int
	}{
		{ProtoSSH, false, 22},
		{ProtoWinRM, false, 5985},
		{ProtoWinRM, true, 5986},
		{ProtoRDP, false, 3389},
	}
	for _, c := range cases {
		if got := DefaultPort(c.p, c.https); got != c.want {
			t.Errorf("DefaultPort(%s,%v) = %d, want %d", c.p, c.https, got, c.want)
		}
	}
}

func TestSSHBuildAuthRejectsMissingCredentials(t *testing.T) {
	if _, err := buildSSHAuth(Target{Auth: AuthPassword}); err == nil {
		t.Error("expected error for empty password")
	}
	if _, err := buildSSHAuth(Target{Auth: AuthPrivateKey}); err == nil {
		t.Error("expected error for empty key")
	}
	if _, err := buildSSHAuth(Target{Auth: "garbage"}); err == nil || !errors.Is(err, err) {
		t.Error("expected error for unknown auth")
	}
}

func TestRDPProbeRequiresHost(t *testing.T) {
	c := NewRDPConnector()
	if _, err := c.Test(context.Background(), Target{Protocol: ProtoRDP}); err == nil {
		t.Fatal("expected host-required error")
	}
}

func TestWinRMRequiresPasswordAuth(t *testing.T) {
	c := NewWinRMConnector()
	if _, err := c.Test(context.Background(), Target{Protocol: ProtoWinRM, Host: "h", Auth: AuthPrivateKey}); err == nil {
		t.Fatal("expected auth-method error")
	}
}
