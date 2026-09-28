ALTER TABLE jobs ADD COLUMN total_files integer NOT NULL DEFAULT 0, ADD COLUMN processed_files integer NOT NULL DEFAULT 0;
CREATE INDEX jobs_library_scan ON jobs(resource_id, created_at DESC) WHERE kind='scan';
