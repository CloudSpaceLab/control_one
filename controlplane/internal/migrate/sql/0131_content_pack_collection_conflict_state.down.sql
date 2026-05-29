UPDATE content_pack_source_runtime_states
SET coverage_state = 'backpressured',
    last_error = CASE
        WHEN last_error = '' THEN 'downgraded from collection_conflict during rollback'
        ELSE last_error
    END
WHERE coverage_state = 'collection_conflict';

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
        'unsupported',
        'privacy_blocked',
        'stale'
    ));
