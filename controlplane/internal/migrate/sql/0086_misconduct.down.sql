-- Rollback for UC7: Misconduct & Whistleblowing.
DROP TABLE IF EXISTS risk_signals CASCADE;
DROP TABLE IF EXISTS case_evidence CASCADE;
DROP TABLE IF EXISTS whistleblower_submissions CASCADE;
DROP TABLE IF EXISTS misconduct_cases CASCADE;

DELETE FROM roles WHERE name = 'investigator';
