ALTER TABLE media_files DROP CONSTRAINT media_files_item_path_key;
ALTER TABLE media_files ADD CONSTRAINT media_files_path_key UNIQUE(path);
