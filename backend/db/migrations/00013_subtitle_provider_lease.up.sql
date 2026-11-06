ALTER TABLE subtitle_provider_state
 ADD COLUMN lease_id text NOT NULL DEFAULT '',
 ADD COLUMN lease_until timestamptz NOT NULL DEFAULT 'epoch';
