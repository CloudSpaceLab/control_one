package sessionrecording

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/internal/api"
)

// Service manages session recording using tlog, auditx, and optionally OpenReplay
type Service struct {
	log            *zap.Logger
	client         *api.Client
	nodeID         string
	cfg            Config
	activeSessions map[string]*Session
	mu             sync.RWMutex
}

// Config holds session recording configuration
type Config struct {
	Enabled          bool
	Backend          string
	StoragePath      string
	RetentionDays    int
	MaxSizeMB        int
	Compress         bool
	UploadInterval   time.Duration
	SessionTypes     []string
	RecordSSH        bool
	RecordTerminal   bool
	RecordCommands   bool
	TlogPath         string
	AuditxPath       string
	OpenReplayAPIKey string
	OpenReplayURL    string
}

// Session represents an active recording session
type Session struct {
	ID           string
	Type         string
	UserID       string
	StartedAt    time.Time
	Process      *exec.Cmd
	ArtifactPath string
	Metadata     map[string]any
}

// NewService creates a new session recording service
func NewService(log *zap.Logger, client *api.Client, nodeID string, cfg Config) *Service {
	return &Service{
		log:            log,
		client:         client,
		nodeID:         nodeID,
		cfg:            cfg,
		activeSessions: make(map[string]*Session),
	}
}

// StartSession begins recording a new session
func (s *Service) StartSession(ctx context.Context, sessionType, userID string, metadata map[string]any) (string, error) {
	if !s.cfg.Enabled {
		return "", fmt.Errorf("session recording is disabled")
	}

	if !s.isSessionTypeEnabled(sessionType) {
		return "", fmt.Errorf("session type %s is not enabled", sessionType)
	}

	sessionID := uuid.New().String()
	artifactPath := filepath.Join(s.cfg.StoragePath, fmt.Sprintf("session-%s-%d.rec", sessionID, time.Now().Unix()))

	session := &Session{
		ID:           sessionID,
		Type:         sessionType,
		UserID:       userID,
		StartedAt:    time.Now(),
		ArtifactPath: artifactPath,
		Metadata:     metadata,
	}

	var cmd *exec.Cmd
	var err error

	switch strings.ToLower(s.cfg.Backend) {
	case "tlog":
		cmd, err = s.startTlogRecording(ctx, session)
	case "auditx":
		cmd, err = s.startAuditxRecording(ctx, session)
	default:
		return "", fmt.Errorf("unsupported recording backend: %s", s.cfg.Backend)
	}

	if err != nil {
		return "", fmt.Errorf("start recording: %w", err)
	}

	session.Process = cmd
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("start recording process: %w", err)
	}

	s.mu.Lock()
	s.activeSessions[sessionID] = session
	s.mu.Unlock()

	if err := s.notifyControlPlane(ctx, sessionID, sessionType, userID, "started", metadata); err != nil {
		s.log.Warn("failed to notify control plane of session start", zap.Error(err))
	}

	s.log.Info("session recording started",
		zap.String("session_id", sessionID),
		zap.String("type", sessionType),
		zap.String("user_id", userID),
		zap.String("backend", s.cfg.Backend))

	return sessionID, nil
}

// StopSession stops recording a session
func (s *Service) StopSession(ctx context.Context, sessionID string) error {
	s.mu.Lock()
	session, exists := s.activeSessions[sessionID]
	if !exists {
		s.mu.Unlock()
		return fmt.Errorf("session %s not found", sessionID)
	}
	delete(s.activeSessions, sessionID)
	s.mu.Unlock()

	if session.Process != nil {
		if err := session.Process.Process.Kill(); err != nil {
			s.log.Warn("failed to kill recording process", zap.Error(err))
		}
		session.Process.Wait()
	}

	duration := time.Since(session.StartedAt)
	artifactSize := int64(0)
	if info, err := os.Stat(session.ArtifactPath); err == nil {
		artifactSize = info.Size()
	}

	if err := s.notifyControlPlane(ctx, sessionID, session.Type, session.UserID, "stopped", map[string]any{
		"duration_seconds": int(duration.Seconds()),
		"artifact_path":    session.ArtifactPath,
		"artifact_size":    artifactSize,
	}); err != nil {
		s.log.Warn("failed to notify control plane of session stop", zap.Error(err))
	}

	if s.cfg.OpenReplayAPIKey != "" && s.cfg.OpenReplayURL != "" {
		if err := s.uploadToOpenReplay(ctx, session); err != nil {
			s.log.Warn("failed to upload to OpenReplay", zap.Error(err))
		}
	}

	s.log.Info("session recording stopped",
		zap.String("session_id", sessionID),
		zap.Duration("duration", duration))

	return nil
}

// ListActiveSessions returns all active recording sessions
func (s *Service) ListActiveSessions() []Session {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sessions := make([]Session, 0, len(s.activeSessions))
	for _, session := range s.activeSessions {
		sessions = append(sessions, *session)
	}
	return sessions
}

// NotifyControlPlane is exported for use by interceptor
func (s *Service) NotifyControlPlane(ctx context.Context, sessionID, sessionType, userID, status string, metadata map[string]any) error {
	return s.notifyControlPlane(ctx, sessionID, sessionType, userID, status, metadata)
}

func (s *Service) isSessionTypeEnabled(sessionType string) bool {
	for _, t := range s.cfg.SessionTypes {
		if strings.EqualFold(t, sessionType) {
			return true
		}
	}
	return false
}

func (s *Service) startTlogRecording(ctx context.Context, session *Session) (*exec.Cmd, error) {
	if s.cfg.TlogPath == "" {
		s.cfg.TlogPath = "/usr/bin/tlog-rec"
	}

	if _, err := exec.LookPath(s.cfg.TlogPath); err != nil {
		return nil, fmt.Errorf("tlog not found at %s", s.cfg.TlogPath)
	}

	args := []string{
		"-w", session.ArtifactPath,
	}

	if s.cfg.Compress {
		args = append(args, "-z")
	}

	cmd := exec.CommandContext(ctx, s.cfg.TlogPath, args...)
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, fmt.Sprintf("TLOG_SESSION_ID=%s", session.ID))
	cmd.Env = append(cmd.Env, fmt.Sprintf("TLOG_USER_ID=%s", session.UserID))

	return cmd, nil
}

func (s *Service) startAuditxRecording(ctx context.Context, session *Session) (*exec.Cmd, error) {
	if s.cfg.AuditxPath == "" {
		s.cfg.AuditxPath = "/usr/bin/auditx"
	}

	if _, err := exec.LookPath(s.cfg.AuditxPath); err != nil {
		return nil, fmt.Errorf("auditx not found at %s", s.cfg.AuditxPath)
	}

	args := []string{
		"record",
		"--output", session.ArtifactPath,
		"--session-id", session.ID,
		"--user-id", session.UserID,
	}

	if s.cfg.RecordCommands {
		args = append(args, "--commands")
	}

	cmd := exec.CommandContext(ctx, s.cfg.AuditxPath, args...)
	cmd.Env = os.Environ()

	return cmd, nil
}

func (s *Service) notifyControlPlane(ctx context.Context, sessionID, sessionType, userID, status string, metadata map[string]any) error {
	if s.client == nil {
		return nil
	}

	payload := map[string]any{
		"node_id":      s.nodeID,
		"session_id":   sessionID,
		"session_type": sessionType,
		"user_id":      userID,
		"status":       status,
		"metadata":     metadata,
	}

	if status == "started" {
		payload["started_at"] = time.Now().UTC().Format(time.RFC3339)
	} else if status == "stopped" {
		payload["ended_at"] = time.Now().UTC().Format(time.RFC3339)
		if duration, ok := metadata["duration_seconds"].(int); ok {
			payload["duration_seconds"] = duration
		}
		if artifactPath, ok := metadata["artifact_path"].(string); ok {
			payload["artifact_path"] = artifactPath
		}
		if artifactSize, ok := metadata["artifact_size"].(int64); ok {
			payload["artifact_size_bytes"] = artifactSize
		}
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	resp, err := s.client.Do(ctx, "POST", "/api/v1/sessions", body)
	if err != nil {
		return fmt.Errorf("notify control plane: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("control plane returned status %d", resp.StatusCode)
	}

	return nil
}

func (s *Service) uploadToOpenReplay(ctx context.Context, session *Session) error {
	if s.cfg.OpenReplayAPIKey == "" || s.cfg.OpenReplayURL == "" {
		return nil
	}

	file, err := os.Open(session.ArtifactPath)
	if err != nil {
		return fmt.Errorf("open artifact: %w", err)
	}
	defer file.Close()

	// OpenReplay upload would go here
	// This is a placeholder for the actual OpenReplay integration
	s.log.Info("OpenReplay upload placeholder",
		zap.String("session_id", session.ID),
		zap.String("url", s.cfg.OpenReplayURL))

	return nil
}
