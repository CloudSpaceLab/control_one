ALTER TABLE content_pack_source_proposals
    ADD COLUMN IF NOT EXISTS collect_mode TEXT NOT NULL DEFAULT '';

ALTER TABLE content_pack_source_proposals
    DROP CONSTRAINT IF EXISTS content_pack_source_proposals_collect_mode_check,
    ADD CONSTRAINT content_pack_source_proposals_collect_mode_check
        CHECK (collect_mode IN ('', 'observe_only', 'metadata_only', 'collect_parsed', 'collect_raw', 'disabled'));
