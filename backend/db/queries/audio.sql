-- name: CanImport :one
SELECT EXISTS(SELECT 1 FROM users u JOIN libraries l ON l.id=sqlc.arg(library_id)
WHERE u.id=sqlc.arg(user_id) AND NOT u.disabled AND l.kind IN ('music','podcasts','audiobooks')
AND (u.role='admin' OR EXISTS(SELECT 1 FROM library_access a WHERE a.user_id=u.id AND a.library_id=l.id AND a.can_import)))::boolean;
-- name: GrantImport :exec
UPDATE library_access SET can_import=true WHERE user_id=$1 AND library_id=$2;
-- name: UserImports :many
SELECT library_id FROM library_access WHERE user_id=$1 AND can_import;
-- name: SaveAudioMetadata :exec
INSERT INTO audio_metadata(item_id,tags,chapters) VALUES($1,$2,$3)
ON CONFLICT(item_id) DO UPDATE SET tags=excluded.tags,chapters=excluded.chapters;
-- name: GetAudioMetadata :one
SELECT * FROM audio_metadata WHERE item_id=$1;
-- name: UpdateAudioTitle :exec
UPDATE items SET title=$2,sort_title=lower($2),year=$3,poster=$4 WHERE id=$1 AND NOT metadata_locked;
-- name: CreateImportSource :one
INSERT INTO import_sources(id,user_id,library_id,url,title,follow) VALUES($1,$2,$3,$4,$5,$6) RETURNING *;
-- name: GetImportSource :one
SELECT * FROM import_sources WHERE id=$1;
-- name: ListImportSources :many
SELECT s.* FROM import_sources s JOIN users u ON u.id=sqlc.arg(user_id)
WHERE u.role='admin' OR (s.user_id=u.id AND EXISTS(SELECT 1 FROM library_access a WHERE a.user_id=u.id AND a.library_id=s.library_id)) ORDER BY s.created_at DESC;
-- name: UpdateImportSource :one
UPDATE import_sources SET paused=$2,follow=$3 WHERE id=$1 RETURNING *;
-- name: DeleteImportSource :exec
DELETE FROM import_sources WHERE id=$1;
-- name: DueImportSources :many
SELECT * FROM import_sources WHERE follow AND NOT paused AND next_sync_at<=now();
-- name: ScheduleImportSource :exec
UPDATE import_sources SET next_sync_at=now()+interval '6 hours',title=CASE WHEN $2::text='' THEN title ELSE $2 END WHERE id=$1;
-- name: SaveImportEntry :exec
INSERT INTO import_entries(source_id,video_id,title,ordinal) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING;
-- name: ListImportEntries :many
SELECT * FROM import_entries WHERE source_id=$1 ORDER BY ordinal,video_id;
-- name: SetImportEntry :exec
UPDATE import_entries SET state=$3,error=$4 WHERE source_id=$1 AND video_id=$2;
-- name: GetImportedFile :one
SELECT * FROM imported_files WHERE library_id=$1 AND video_id=$2;
-- name: SaveImportedFile :exec
INSERT INTO imported_files(library_id,video_id,path) VALUES($1,$2,$3) ON CONFLICT DO NOTHING;
-- name: SaveTagJob :exec
INSERT INTO audio_tag_jobs(id,user_id,file_id,fingerprint,patch) VALUES($1,$2,$3,$4,$5);
-- name: GetTagJob :one
SELECT * FROM audio_tag_jobs WHERE id=$1;
-- name: SetTagPhase :exec
UPDATE audio_tag_jobs SET phase=$2,error=$3,updated_at=now() WHERE id=$1;
-- name: ListAudioJobs :many
SELECT j.* FROM jobs j LEFT JOIN import_sources s ON s.id=j.resource_id
LEFT JOIN audio_tag_jobs t ON t.id=j.id LEFT JOIN media_files f ON f.id=t.file_id LEFT JOIN items i ON i.id=f.item_id
JOIN users u ON u.id=sqlc.arg(user_id)
WHERE j.kind IN ('youtube_preview','youtube_download','audio_tags')
AND (u.role='admin' OR ((s.user_id=u.id OR t.user_id=u.id) AND EXISTS(SELECT 1 FROM library_access a WHERE a.user_id=u.id AND a.library_id=COALESCE(s.library_id,i.library_id))))
ORDER BY j.created_at DESC LIMIT 100;
-- name: GetJob :one
SELECT * FROM jobs WHERE id=$1;
-- name: ValidJobLease :one
SELECT EXISTS(SELECT 1 FROM jobs WHERE id=$1 AND lease_id=$2 AND state='running' AND NOT cancel_requested AND lease_until>now())::boolean;
-- name: AudioChildren :many
SELECT i.* FROM items i LEFT JOIN audio_collection_members m ON m.item_id=i.id AND m.collection_id=$1
WHERE i.parent_id=$1 OR m.collection_id=$1 ORDER BY i.season,COALESCE(m.ordinal,i.episode),i.sort_title,i.id;
-- name: LockDownloads :exec
SELECT pg_advisory_xact_lock(7311903);
-- name: ActiveDownloads :one
SELECT count(*) FROM jobs WHERE role='downloader' AND state='running' AND lease_until>now();
-- name: ListAudioArtists :many
SELECT DISTINCT artist.value::text AS name FROM audio_metadata a
JOIN items i ON i.id=a.item_id CROSS JOIN LATERAL jsonb_array_elements_text(a.tags->'artists') artist(value)
WHERE i.library_id=$1 AND artist.value<>'' ORDER BY name;

-- name: SetWorkerCapabilities :exec
UPDATE workers SET capabilities=$2 WHERE id=$1;
-- name: AbandonedTagJobs :many
SELECT t.* FROM audio_tag_jobs t JOIN jobs j ON j.id=t.id
WHERE j.state IN ('failed','cancelled') AND t.phase NOT IN ('indexed','abandoned') LIMIT 100;

-- name: LibraryImportSources :many
SELECT * FROM import_sources WHERE library_id=$1;
-- name: SaveAudioMember :exec
INSERT INTO audio_collection_members(collection_id,item_id,ordinal) VALUES($1,$2,$3) ON CONFLICT(collection_id,item_id) DO NOTHING;
-- name: CancelSourceJobs :exec
UPDATE jobs SET cancel_requested=true,state=CASE WHEN state='pending' THEN 'cancelled' ELSE state END
WHERE resource_id=$1 AND role='downloader' AND state IN ('pending','running');
