ALTER TABLE content_pack_source_proposals
    DROP CONSTRAINT IF EXISTS content_pack_source_proposals_collect_mode_check;

ALTER TABLE content_pack_source_proposals
    DROP COLUMN IF EXISTS collect_mode;
