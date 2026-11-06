ALTER TABLE libraries DROP CONSTRAINT libraries_kind_check;
ALTER TABLE libraries ADD CONSTRAINT libraries_kind_check CHECK (kind IN ('movies','series','music','podcasts','audiobooks'));
ALTER TABLE jobs DROP CONSTRAINT jobs_role_check;
ALTER TABLE jobs ADD CONSTRAINT jobs_role_check CHECK (role IN ('scanner','transcoder','downloader'));
ALTER TABLE jobs ADD COLUMN next_attempt_at timestamptz NOT NULL DEFAULT now();
ALTER TABLE library_access ADD COLUMN can_import boolean NOT NULL DEFAULT false;
ALTER TABLE workers ADD COLUMN capabilities jsonb NOT NULL DEFAULT '{}';
CREATE TABLE audio_metadata (
 item_id text PRIMARY KEY REFERENCES items ON DELETE CASCADE,
 tags jsonb NOT NULL DEFAULT '{}', chapters jsonb NOT NULL DEFAULT '[]'
);
CREATE TABLE audio_collection_members (
 collection_id text NOT NULL REFERENCES items ON DELETE CASCADE,
 item_id text NOT NULL REFERENCES items ON DELETE CASCADE, ordinal integer NOT NULL,
 PRIMARY KEY(collection_id,item_id)
);
CREATE TABLE import_sources (
 id text PRIMARY KEY, user_id text NOT NULL REFERENCES users ON DELETE CASCADE,
 library_id text NOT NULL REFERENCES libraries ON DELETE CASCADE,
 url text NOT NULL, title text NOT NULL DEFAULT '', follow boolean NOT NULL DEFAULT false,
 paused boolean NOT NULL DEFAULT false, next_sync_at timestamptz NOT NULL DEFAULT now(),
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE import_entries (
 source_id text NOT NULL REFERENCES import_sources ON DELETE CASCADE,
 video_id text NOT NULL, title text NOT NULL, ordinal integer NOT NULL,
 state text NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','ready','failed')),
 error text NOT NULL DEFAULT '', PRIMARY KEY(source_id,video_id)
);
CREATE TABLE imported_files (
 library_id text NOT NULL REFERENCES libraries ON DELETE CASCADE,
 video_id text NOT NULL, path text NOT NULL, PRIMARY KEY(library_id,video_id)
);
CREATE TABLE audio_tag_jobs (
 id text PRIMARY KEY REFERENCES jobs ON DELETE CASCADE,
 user_id text NOT NULL REFERENCES users ON DELETE CASCADE,
 file_id text NOT NULL REFERENCES media_files ON DELETE CASCADE,
 fingerprint text NOT NULL, patch jsonb NOT NULL,
 phase text NOT NULL DEFAULT 'pending', error text NOT NULL DEFAULT '',
 updated_at timestamptz NOT NULL DEFAULT now()
);
INSERT INTO settings VALUES ('downloads','{"maxConcurrent":1,"minFreeGiB":5}') ON CONFLICT DO NOTHING;
