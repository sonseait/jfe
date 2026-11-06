ALTER TABLE libraries ADD COLUMN auto_subtitles boolean NOT NULL DEFAULT false;
CREATE TABLE automatic_subtitles (
 file_id text PRIMARY KEY REFERENCES media_files ON DELETE CASCADE,
 state text NOT NULL DEFAULT 'pending',
 next_search_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now(),
 provider_file_id integer NOT NULL DEFAULT 0,
 error_code text NOT NULL DEFAULT '',
 publication jsonb NOT NULL DEFAULT '{}'
);
CREATE INDEX automatic_subtitles_due ON automatic_subtitles(next_search_at);
CREATE TABLE subtitle_provider_state (
 id boolean PRIMARY KEY DEFAULT true CHECK(id),
 status text NOT NULL DEFAULT 'unchecked',
 vip boolean NOT NULL DEFAULT false,
 remaining integer NOT NULL DEFAULT -1,
 reset_at timestamptz NOT NULL DEFAULT 'epoch',
 retry_at timestamptz NOT NULL DEFAULT 'epoch',
 checked_at timestamptz NOT NULL DEFAULT 'epoch'
);
INSERT INTO subtitle_provider_state(id) VALUES(true);
