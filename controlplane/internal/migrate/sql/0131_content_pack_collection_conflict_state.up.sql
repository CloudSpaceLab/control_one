ALTER TABLE content_pack_source_runtime_states
    DROP CONSTRAINT IF EXISTS content_pack_source_runtime_states_coverage_state_check;

ALTER TABLE content_pack_source_runtime_states
    ADD CONSTRAINT content_pack_source_runtime_states_coverage_state_check
    CHECK (coverage_state IN (
        'discovered',
        'proposed',
        'approval_required',
        'approved',
        'config_rendered',
        'deployed',
        'collecting',
        'parser_healthy',
        'parser_failed',
        'silent',
        'backpressured',
        'collection_conflict',
        'unsupported',
        'privacy_blocked',
        'stale'
    ));
