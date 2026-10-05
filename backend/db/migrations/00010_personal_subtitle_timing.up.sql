CREATE TABLE personal_subtitle_timing (
 user_id text NOT NULL REFERENCES users(id) ON DELETE CASCADE,
 file_id text NOT NULL REFERENCES media_files(id) ON DELETE CASCADE,
 source_key text NOT NULL,
 offset_ms integer NOT NULL CHECK(offset_ms BETWEEN -600000 AND 600000),
 PRIMARY KEY(user_id,file_id,source_key)
);
