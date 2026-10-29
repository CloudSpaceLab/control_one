ALTER TABLE node_inventory_sync ADD COLUMN snapshot JSONB;
CREATE TABLE vulnerability_publication (
    id INTEGER PRIMARY KEY CHECK(id=1),
    generation TEXT NOT NULL,
    published_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
