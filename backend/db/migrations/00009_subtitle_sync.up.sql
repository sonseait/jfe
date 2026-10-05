CREATE TABLE subtitle_sync_jobs (
 id text PRIMARY KEY REFERENCES jobs(id) ON DELETE CASCADE,
 -- Retain actor identity so already published edits can recover after user deletion.
 user_id text NOT NULL,
 file_id text NOT NULL REFERENCES media_files(id) ON DELETE CASCADE,
 track_index integer NOT NULL,
 probe_revision text NOT NULL,
 action text NOT NULL CHECK(action IN ('prepare','save','delete')),
 fingerprint text NOT NULL DEFAULT '',
 offset_ms integer NOT NULL DEFAULT 0 CHECK(offset_ms BETWEEN -600000 AND 600000),
 source_path text NOT NULL DEFAULT '',
 cues jsonb NOT NULL DEFAULT '[]',
 phase text NOT NULL DEFAULT 'pending',
 error_code text NOT NULL DEFAULT ''
);
CREATE INDEX subtitle_sync_file ON subtitle_sync_jobs(file_id);

ALTER TABLE playback_sessions ADD COLUMN preview boolean NOT NULL DEFAULT false;
