CREATE TABLE IF NOT EXISTS session_recordings (
    id UUID PRIMARY KEY,
    node_id UUID NOT NULL,
    user_id TEXT,
    session_type TEXT NOT NULL DEFAULT 'terminal',
    started_at TIMESTAMPTZ NOT NULL,
    ended_at TIMESTAMPTZ,
    duration_seconds INTEGER,
    status TEXT NOT NULL DEFAULT 'active',
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    artifact_path TEXT,
    artifact_size_bytes BIGINT,
    checksum TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS session_recordings_node_id_idx
    ON session_recordings(node_id);
CREATE INDEX IF NOT EXISTS session_recordings_user_id_idx
    ON session_recordings(user_id);
CREATE INDEX IF NOT EXISTS session_recordings_started_at_idx
    ON session_recordings(started_at);
CREATE INDEX IF NOT EXISTS session_recordings_status_idx
    ON session_recordings(status);
CREATE INDEX IF NOT EXISTS session_recordings_session_type_idx
    ON session_recordings(session_type);

CREATE TABLE IF NOT EXISTS session_events (
    id UUID PRIMARY KEY,
    session_id UUID NOT NULL REFERENCES session_recordings(id) ON DELETE CASCADE,
    event_type TEXT NOT NULL,
    timestamp TIMESTAMPTZ NOT NULL,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS session_events_session_id_idx
    ON session_events(session_id);
CREATE INDEX IF NOT EXISTS session_events_timestamp_idx
    ON session_events(timestamp);
CREATE INDEX IF NOT EXISTS session_events_event_type_idx
    ON session_events(event_type);

COMMENT ON TABLE session_recordings IS 'Stores session recording metadata and artifacts';
COMMENT ON TABLE session_events IS 'Stores individual events within a session recording';

