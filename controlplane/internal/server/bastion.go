package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"

	"github.com/google/uuid"
	"go.uber.org/zap"
	"golang.org/x/crypto/ssh"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/doris"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/eventbus"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/sshproxy"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
)

// startBastionProxy boots the SSH bastion when cfg.Bastion.Enabled. It
// loads the host signer + tenant CA pubkey, builds the mTLS dialer that
// reaches the target node's tunnel listener, and wires EmitSession to
// publish bastion.session.{open,close} events both to the in-memory
// eventbus (live UI SSE) and to Doris (forensic timeline).
//
// Returns nil + logs a warning when material is missing — bastion is
// optional and we never want a misconfiguration to crash the
// controlplane's main event loop.
func (s *Server) startBastionProxy(ctx context.Context) error {
	cfg := s.cfg.Bastion
	if cfg.HostKeyFile == "" || cfg.CAPublicKeyFile == "" {
		s.logger.Warn("bastion enabled but host_key_file or ca_public_key_file missing; skipping")
		return nil
	}
	hostKeyBytes, err := os.ReadFile(cfg.HostKeyFile)
	if err != nil {
		return fmt.Errorf("read bastion host key: %w", err)
	}
	hostSigner, err := ssh.ParsePrivateKey(hostKeyBytes)
	if err != nil {
		return fmt.Errorf("parse bastion host key: %w", err)
	}
	caBytes, err := os.ReadFile(cfg.CAPublicKeyFile)
	if err != nil {
		return fmt.Errorf("read bastion ca pubkey: %w", err)
	}
	caKey, _, _, _, err := ssh.ParseAuthorizedKey(caBytes)
	if err != nil {
		return fmt.Errorf("parse bastion ca pubkey: %w", err)
	}

	dialer, err := sshproxy.NewMTLSDialer(sshproxy.MTLSDialerConfig{
		Store:          bastionDialerStore{store: s.store},
		ClientCertFile: s.cfg.TLS.CertFile,
		ClientKeyFile:  s.cfg.TLS.KeyFile,
		CACertFile:     s.cfg.TLS.ClientCAFile,
	})
	if err != nil {
		return fmt.Errorf("init bastion dialer: %w", err)
	}

	emit := bastionEmitWiring(s.eventBus, s.dorisWriter, s.logger)

	proxy, err := sshproxy.New(sshproxy.Config{
		CAPublicKey: caKey,
		HostSigner:  hostSigner,
		NodeDialer:  dialer,
		Log:         s.logger.Named("bastion"),
		EmitSession: emit,
	})
	if err != nil {
		return fmt.Errorf("init bastion proxy: %w", err)
	}

	listenAddr := cfg.ListenAddr
	if listenAddr == "" {
		listenAddr = ":2200"
	}
	listener, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return fmt.Errorf("listen bastion %s: %w", listenAddr, err)
	}
	go func() {
		if serveErr := proxy.Serve(ctx, listener); serveErr != nil {
			s.logger.Error("bastion serve", zap.Error(serveErr))
		}
	}()
	s.logger.Info("bastion proxy listening", zap.String("addr", listenAddr))
	return nil
}

// bastionDialerStore narrows our Store interface (which has many methods)
// to the GetNode-only surface MTLSDialer expects.
type bastionDialerStore struct {
	store interface {
		GetNode(context.Context, uuid.UUID) (*storage.Node, error)
	}
}

func (b bastionDialerStore) GetNode(ctx context.Context, id uuid.UUID) (*storage.Node, error) {
	return b.store.GetNode(ctx, id)
}

// bastionEmitWiring returns an EmitSession callback that fans every
// open/close into the eventbus (live UI) and the Doris writer (forensic
// timeline). NodeID is in the event payload; tenant id is resolved at
// query time by joining `nodes` on `node_id`.
func bastionEmitWiring(bus *eventbus.Bus, writer *doris.Writer, log *zap.Logger) func(sshproxy.BastionSessionEvent) {
	return func(e sshproxy.BastionSessionEvent) {
		ts := e.StartedAt
		if e.Kind == "close" && !e.EndedAt.IsZero() {
			ts = e.EndedAt
		}
		dur := int64(0)
		if !e.EndedAt.IsZero() {
			dur = e.EndedAt.Sub(e.StartedAt).Milliseconds()
		}

		// 1. Live SSE topic for the UI.
		if bus != nil {
			payload, _ := json.Marshal(map[string]any{
				"kind":        e.Kind,
				"session_id":  e.SessionID,
				"principal":   e.Principal,
				"node_id":     e.NodeID,
				"src_ip":      e.RemoteIP,
				"src_port":    e.RemotePort,
				"bytes_up":    e.BytesUp,
				"bytes_down":  e.BytesDown,
				"started_at":  e.StartedAt,
				"ended_at":    e.EndedAt,
				"duration_ms": dur,
			})
			ev := eventbus.Event{
				ID:        uuid.New(),
				Topic:     "events.bastion",
				Timestamp: ts,
				Payload:   payload,
			}
			if e.NodeID != "" {
				if parsed, err := uuid.Parse(e.NodeID); err == nil {
					ev.NodeID = &parsed
				}
			}
			bus.Publish(ev)
		}

		// 2. Doris row → forensic timeline.
		if writer != nil {
			row := map[string]any{
				"event_date":         ts.UTC().Format("2006-01-02"),
				"node_id":            e.NodeID,
				"ts":                 ts.UTC().Format("2006-01-02 15:04:05.000"),
				"event_type":         "bastion.session." + e.Kind,
				"severity":           "info",
				"correlation_id":     e.SessionID,
				"bastion_session_id": e.SessionID,
				"user_name":          e.Principal,
				"src_ip":             e.RemoteIP,
				"src_port":           e.RemotePort,
				"bytes_in":           e.BytesDown,
				"bytes_out":          e.BytesUp,
				"duration_ms":        dur,
				"message":            fmt.Sprintf("bastion %s by %s from %s", e.Kind, e.Principal, e.RemoteIP),
				"dedup_key":          fmt.Sprintf("bastion.%s:%s", e.Kind, e.SessionID),
			}
			if err := writer.Enqueue("events", []map[string]any{row}); err != nil {
				if log != nil {
					log.Warn("bastion doris enqueue", zap.Error(err))
				}
			}
		}
	}
}
