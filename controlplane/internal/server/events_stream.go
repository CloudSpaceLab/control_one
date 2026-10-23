package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/eventbus"
)

// handleEventsStream upgrades the connection to a Server-Sent Events stream.
// Query params:
//
//	tenant_id (required)  — scopes subscription
//	node_id   (optional)  — narrow to one node
//	topics    (optional)  — comma-separated topic filter; default all
//
// The connection stays open until the client disconnects or the server is
// shutting down. A 20-second keep-alive comment is sent so intermediaries do
// not drop the idle connection.
func (s *Server) handleEventsStream(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	principal, ok := s.authorize(w, r)
	if !ok {
		return
	}
	if s.eventBus == nil {
		http.Error(w, "event bus not initialized", http.StatusServiceUnavailable)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	tenantParam := strings.TrimSpace(r.URL.Query().Get("tenant_id"))
	if tenantParam == "" {
		http.Error(w, "tenant_id is required", http.StatusBadRequest)
		return
	}
	tenantID, err := uuid.Parse(tenantParam)
	if err != nil {
		http.Error(w, "invalid tenant_id", http.StatusBadRequest)
		return
	}

	var nodeFilter *uuid.UUID
	if nodeParam := strings.TrimSpace(r.URL.Query().Get("node_id")); nodeParam != "" {
		nodeID, err := uuid.Parse(nodeParam)
		if err != nil {
			http.Error(w, "invalid node_id", http.StatusBadRequest)
			return
		}
		nodeFilter = &nodeID
	}

	var topics []string
	if topicsParam := strings.TrimSpace(r.URL.Query().Get("topics")); topicsParam != "" {
		for _, t := range strings.Split(topicsParam, ",") {
			t = strings.TrimSpace(t)
			if t != "" {
				topics = append(topics, t)
			}
		}
	}

	if principal.Type == "agent" {
		if s.store == nil {
			http.Error(w, "storage unavailable", http.StatusServiceUnavailable)
			return
		}
		agentTenant, agentNode, err := s.tenantNodeForAgent(r.Context(), principal)
		if err != nil || tenantID != agentTenant || nodeFilter == nil || *nodeFilter != agentNode {
			http.Error(w, "agent stream must target its registered tenant and node", http.StatusForbidden)
			return
		}
		if len(topics) == 0 {
			http.Error(w, "agent stream requires explicit topics", http.StatusForbidden)
			return
		}
		for _, topic := range topics {
			if topic != eventbus.TopicPolicyUpdated && topic != eventbus.TopicRuleTriggered {
				http.Error(w, "agent stream topic not allowed", http.StatusForbidden)
				return
			}
		}
	} else {
		if _, ok := s.authorize(w, r, roleViewer); !ok {
			return
		}
		if !s.requireTenantAccess(w, r, principal, tenantID, roleViewer) {
			return
		}
	}

	// Give each streaming write a bounded deadline. The ordinary HTTP write
	// timeout otherwise terminates this long-lived connection between heartbeats.
	controller := http.NewResponseController(w)
	refreshDeadline := func() error {
		err := controller.SetWriteDeadline(time.Now().Add(30 * time.Second))
		if errors.Is(err, http.ErrNotSupported) {
			return nil
		} // e.g. recorder tests
		return err
	}
	if err := refreshDeadline(); err != nil {
		http.Error(w, "stream deadline unavailable", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintf(w, ": connected tenant=%s\n\n", tenantID)
	flusher.Flush()

	sub := s.eventBus.Subscribe(tenantID, topics, nodeFilter)
	defer sub.Close()

	keepAlive := time.NewTicker(20 * time.Second)
	defer keepAlive.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-keepAlive.C:
			if err := refreshDeadline(); err != nil {
				return
			}
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case ev, ok := <-sub.Ch:
			if !ok {
				return
			}
			// The bus also supports global broadcasts. This authenticated route
			// delivers only explicitly tenant-scoped events.
			if ev.TenantID != tenantID {
				continue
			}
			if err := refreshDeadline(); err != nil {
				return
			}
			if err := writeSSEEvent(w, ev); err != nil {
				s.logger.Debug("sse write failed", zap.Error(err))
				return
			}
			flusher.Flush()
		}
	}
}

func writeSSEEvent(w http.ResponseWriter, ev eventbus.Event) error {
	data, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "id: %s\nevent: %s\ndata: %s\n\n", ev.ID, ev.Topic, data); err != nil {
		return err
	}
	return nil
}
