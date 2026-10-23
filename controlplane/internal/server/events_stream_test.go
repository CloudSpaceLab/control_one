package server

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/eventbus"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

func TestEventStreamSurvivesOrdinaryWriteTimeout(t *testing.T) {
	tenant, node := uuid.New(), uuid.New()
	srv := &Server{store: &fakeStore{nodes: []storage.Node{{ID: node, TenantID: tenant}}}, logger: zap.NewNop(), eventBus: eventbus.New(8)}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = withPrincipal(r, &auth.Principal{Type: "agent", Subject: node.String(), Roles: []string{"agent"}})
		srv.handleEventsStream(w, r)
	})
	server := httptest.NewUnstartedServer(loggingMiddleware(zap.NewNop(), handler))
	server.Config.WriteTimeout = 50 * time.Millisecond
	server.Start()
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"?tenant_id="+tenant.String()+"&node_id="+node.String()+"&topics=policy.updated", nil)
	resp, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	reader := bufio.NewReader(resp.Body)
	if _, err = reader.ReadString('\n'); err != nil {
		t.Fatal(err)
	}
	reader.ReadString('\n')
	time.Sleep(100 * time.Millisecond)
	srv.eventBus.Publish(eventbus.Event{TenantID: tenant, NodeID: &node, Topic: eventbus.TopicPolicyUpdated})
	line, err := reader.ReadString('\n')
	if err != nil || !strings.HasPrefix(line, "id:") {
		t.Fatalf("stream ended at ordinary write timeout: line=%q error=%v", line, err)
	}
}

func TestAgentEventStreamScope(t *testing.T) {
	tenant, node := uuid.New(), uuid.New()
	srv := &Server{store: &fakeStore{nodes: []storage.Node{{ID: node, TenantID: tenant}}}, logger: zap.NewNop(), eventBus: eventbus.New(8)}
	for _, tc := range []struct {
		name, tenant, node, topics string
		want                       int
	}{
		{"own node", tenant.String(), node.String(), "policy.updated,rule.triggered", 200},
		{"other tenant", uuid.NewString(), node.String(), "policy.updated", 403},
		{"other node", tenant.String(), uuid.NewString(), "policy.updated", 403},
		{"tenant wide", tenant.String(), "", "policy.updated", 403},
		{"all topics", tenant.String(), node.String(), "", 403},
		{"sensitive topic", tenant.String(), node.String(), "security.event", 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			req := httptest.NewRequest(http.MethodGet, "/api/v1/events/stream?tenant_id="+tc.tenant+"&node_id="+tc.node+"&topics="+tc.topics, nil).WithContext(ctx)
			req = withPrincipal(req, &auth.Principal{Type: "agent", Subject: node.String(), Roles: []string{"agent"}})
			rec := httptest.NewRecorder()
			srv.handleEventsStream(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			if srv.eventBus.SubscriberCount() != 0 {
				t.Fatal("stream subscription leaked")
			}
		})
	}
}

func TestEventStreamRejectsUnassignedUserTenant(t *testing.T) {
	tenant, node, user := uuid.New(), uuid.New(), uuid.New()
	store := &patchSecurityStore{patchTestStore: newPatchTestStore(tenant, node)}
	store.users = map[string]*storage.User{"stream-user": {ID: user, ExternalID: "stream-user"}}
	srv := &Server{store: store, logger: zap.NewNop(), eventBus: eventbus.New(8)}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/events/stream?tenant_id="+tenant.String(), nil)
	req = withPrincipal(req, &auth.Principal{Type: "user", Subject: "stream-user", Roles: []string{roleViewer}})
	rec := httptest.NewRecorder()
	srv.handleEventsStream(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
