-- Reverse 0029_node_cert_rotation.up.sql
DROP INDEX IF EXISTS idx_node_cert_history_node;
DROP TABLE IF EXISTS node_certificate_history;

ALTER TABLE nodes DROP COLUMN IF EXISTS cert_rotated_at;
ALTER TABLE nodes DROP COLUMN IF EXISTS cert_serial;
