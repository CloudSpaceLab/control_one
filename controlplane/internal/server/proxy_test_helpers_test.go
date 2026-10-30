package server

import (
	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"go.uber.org/zap"
)

// Endpoint/receipt fixtures must not launch retention/replay goroutines against
// mutable in-memory rows. Production New still starts its normal schedulers.
func newProxyTestServer(store Store) *Server {
	s := New(zap.NewNop(), &config.Config{HTTP: config.HTTPConfig{Address: ":0"}}, nil, &stubQueue{})
	s.store = store
	return s
}
