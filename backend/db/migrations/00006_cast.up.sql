ALTER TABLE items ADD COLUMN cast_members jsonb NOT NULL DEFAULT '[]'::jsonb
    CHECK (jsonb_typeof(cast_members) = 'array');
CREATE INDEX items_cast_members_idx ON items USING gin (cast_members jsonb_path_ops);
