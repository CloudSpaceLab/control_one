// Package events lets the nodeagent subscribe to the control-plane SSE stream
// and react to realtime events (policy.updated, rule.triggered, ...). The
// subscriber retries with backoff on disconnect so a flapping control plane
// does not stall the agent.
package events

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/internal/api"
)

// Event mirrors controlplane/internal/eventbus.Event. We redeclare it here to
// avoid importing the control-plane module from the nodeagent.
type Event struct {
	ID        string          `json:"id"`
	Topic     string          `json:"topic"`
	TenantID  string          `json:"tenant_id"`
	NodeID    string          `json:"node_id,omitempty"`
	Payload   json.RawMessage `json:"payload,omitempty"`
	Timestamp time.Time       `json:"timestamp"`
}

// Handler is invoked for each received event. Implementations should return
// quickly; long-running work belongs on a separate goroutine.
type Handler func(ctx context.Context, ev Event)

// Subscriber consumes the server-sent events stream.
type Subscriber struct {
	client   *api.Client
	log      *zap.Logger
	tenantID string
	nodeID   string
	topics   []string
	handler  Handler
}

// Options configures a Subscriber.
type Options struct {
	TenantID string
	NodeID   string
	Topics   []string
	Handler  Handler
}

// New returns a ready Subscriber.
func New(client *api.Client, log *zap.Logger, opts Options) (*Subscriber, error) {
	if client == nil {
		return nil, fmt.Errorf("api client required")
	}
	if opts.TenantID == "" {
		return nil, fmt.Errorf("tenant id required")
	}
	if opts.Handler == nil {
		return nil, fmt.Errorf("handler required")
	}
	return &Subscriber{
		client:   client,
		log:      log,
		tenantID: opts.TenantID,
		nodeID:   opts.NodeID,
		topics:   opts.Topics,
		handler:  opts.Handler,
	}, nil
}

// Run loops, reconnecting with exponential backoff until ctx is cancelled.
func (s *Subscriber) Run(ctx context.Context) {
	backoff := time.Second
	for {
		if ctx.Err() != nil {
			return
		}
		err := s.connect(ctx)
		if ctx.Err() != nil {
			return
		}
		if err != nil && s.log != nil {
			// Suppress log spam when the error is just context cancellation during
			// connect — the outer ctx.Err() check above already handles shutdown,
			// but a racing cancel may surface as a network error here.
			s.log.Warn("event stream disconnected", zap.Error(err), zap.Duration("backoff", backoff))
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > 30*time.Second {
			backoff = 30 * time.Second
		}
	}
}

func (s *Subscriber) connect(ctx context.Context) error {
	q := url.Values{}
	q.Set("tenant_id", s.tenantID)
	if s.nodeID != "" {
		q.Set("node_id", s.nodeID)
	}
	if len(s.topics) > 0 {
		q.Set("topics", strings.Join(s.topics, ","))
	}
	path := "/api/v1/events/stream?" + q.Encode()

	resp, err := s.client.Stream(ctx, path)
	if err != nil {
		return fmt.Errorf("stream dial: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		return fmt.Errorf("stream status %d", resp.StatusCode)
	}

	return s.readLoop(ctx, resp.Body)
}

func (s *Subscriber) readLoop(ctx context.Context, body io.Reader) error {
	reader := bufio.NewReader(body)
	var dataBuf strings.Builder
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		line, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			if dataBuf.Len() > 0 {
				var ev Event
				if jerr := json.Unmarshal([]byte(dataBuf.String()), &ev); jerr == nil {
					s.handler(ctx, ev)
				} else if s.log != nil {
					s.log.Debug("sse decode failed", zap.Error(jerr))
				}
				dataBuf.Reset()
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		if strings.HasPrefix(line, "data: ") {
			dataBuf.WriteString(strings.TrimPrefix(line, "data: "))
		}
	}
}
