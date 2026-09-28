CREATE TABLE users (
 id text PRIMARY KEY, username text NOT NULL UNIQUE, password_hash text NOT NULL,
 role text NOT NULL CHECK(role IN ('admin','user')), disabled boolean NOT NULL DEFAULT false,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE sessions (token_hash text PRIMARY KEY, user_id text NOT NULL REFERENCES users ON DELETE CASCADE, expires_at timestamptz NOT NULL);
CREATE TABLE libraries (id text PRIMARY KEY, name text NOT NULL, kind text NOT NULL CHECK(kind IN ('movies','series')), paths text[] NOT NULL, scan_interval_hours integer NOT NULL DEFAULT 0 CHECK(scan_interval_hours>=0), last_scan_at timestamptz NOT NULL DEFAULT '1970-01-01');
CREATE TABLE library_access (user_id text NOT NULL REFERENCES users ON DELETE CASCADE, library_id text NOT NULL REFERENCES libraries ON DELETE CASCADE, PRIMARY KEY(user_id,library_id));
CREATE TABLE items (
 id text PRIMARY KEY, library_id text NOT NULL REFERENCES libraries ON DELETE CASCADE,
 parent_id text NOT NULL DEFAULT '', kind text NOT NULL, title text NOT NULL, sort_title text NOT NULL,
 year integer NOT NULL DEFAULT 0, season integer NOT NULL DEFAULT 0, episode integer NOT NULL DEFAULT 0,
 overview text NOT NULL DEFAULT '', poster text NOT NULL DEFAULT '', provider_id text NOT NULL DEFAULT '',
 metadata_locked boolean NOT NULL DEFAULT false, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX items_catalog ON items(library_id,sort_title,id);
CREATE INDEX items_parent ON items(parent_id,season,episode);
CREATE TABLE media_files (
 id text PRIMARY KEY, item_id text NOT NULL REFERENCES items ON DELETE CASCADE,
 path text NOT NULL UNIQUE, size bigint NOT NULL, modified_at bigint NOT NULL, duration double precision NOT NULL DEFAULT 0,
 probe jsonb NOT NULL DEFAULT '{}', available boolean NOT NULL DEFAULT true
);
CREATE TABLE user_state (
 user_id text NOT NULL REFERENCES users ON DELETE CASCADE, item_id text NOT NULL REFERENCES items ON DELETE CASCADE,
 favorite boolean NOT NULL DEFAULT false, watched boolean NOT NULL DEFAULT false,
 position double precision NOT NULL DEFAULT 0, updated_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(user_id,item_id)
);
CREATE TABLE jobs (
 id text PRIMARY KEY, role text NOT NULL CHECK(role IN ('scanner','transcoder')), kind text NOT NULL,
 resource_id text NOT NULL, state text NOT NULL DEFAULT 'pending' CHECK(state IN ('pending','running','completed','failed','cancelled')),
 payload jsonb NOT NULL DEFAULT '{}', error text NOT NULL DEFAULT '', progress integer NOT NULL DEFAULT 0,
 attempts integer NOT NULL DEFAULT 0, lease_id text NOT NULL DEFAULT '', lease_until timestamptz NOT NULL DEFAULT '1970-01-01',
 cancel_requested boolean NOT NULL DEFAULT false, created_at timestamptz NOT NULL DEFAULT now(), updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX one_active_job ON jobs(kind,resource_id) WHERE state IN ('pending','running');
CREATE INDEX jobs_claim ON jobs(role,state,created_at);
CREATE TABLE workers (id text PRIMARY KEY, role text NOT NULL, heartbeat_at timestamptz NOT NULL DEFAULT now());
CREATE TABLE playback_sessions (
 id text PRIMARY KEY, user_id text NOT NULL REFERENCES users ON DELETE CASCADE, item_id text NOT NULL REFERENCES items ON DELETE CASCADE,
 file_id text NOT NULL REFERENCES media_files ON DELETE CASCADE, token_hash text NOT NULL,
 method text NOT NULL, state text NOT NULL, start_position double precision NOT NULL DEFAULT 0,
 sequence bigint NOT NULL DEFAULT 0, position double precision NOT NULL DEFAULT 0,
 expires_at timestamptz NOT NULL, updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE settings (key text PRIMARY KEY, value jsonb NOT NULL);
INSERT INTO settings VALUES ('encoding','{"threads":2,"crf":23,"maxConcurrent":1}');
