-- Refuse rollback while audio data exists; never silently discard imported media records.
DO $$ BEGIN
 IF EXISTS (SELECT 1 FROM libraries WHERE kind IN ('music','podcasts','audiobooks')) THEN
  RAISE EXCEPTION 'Remove audio libraries before rolling back audio support';
 END IF;
END $$;
DROP TABLE audio_collection_members, audio_tag_jobs, imported_files, import_entries, import_sources, audio_metadata;
DELETE FROM settings WHERE key='downloads';
ALTER TABLE workers DROP COLUMN capabilities;
ALTER TABLE jobs DROP COLUMN next_attempt_at;
ALTER TABLE library_access DROP COLUMN can_import;
DELETE FROM jobs WHERE role='downloader' OR kind='audio_tags';
ALTER TABLE jobs DROP CONSTRAINT jobs_role_check;
ALTER TABLE jobs ADD CONSTRAINT jobs_role_check CHECK(role IN ('scanner','transcoder'));
ALTER TABLE libraries DROP CONSTRAINT libraries_kind_check;
ALTER TABLE libraries ADD CONSTRAINT libraries_kind_check CHECK(kind IN ('movies','series'));
