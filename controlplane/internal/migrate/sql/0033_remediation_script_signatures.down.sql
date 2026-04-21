ALTER TABLE remediation_scripts
    DROP COLUMN IF EXISTS signature_algorithm,
    DROP COLUMN IF EXISTS signature;
