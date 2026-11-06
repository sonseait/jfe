CREATE TABLE uploaded_subtitles (
 id text PRIMARY KEY,
 file_id text NOT NULL REFERENCES media_files ON DELETE CASCADE,
 user_id text NOT NULL REFERENCES users ON DELETE CASCADE,
 name text NOT NULL,
 cues jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX uploaded_subtitles_owner ON uploaded_subtitles(file_id,user_id);
