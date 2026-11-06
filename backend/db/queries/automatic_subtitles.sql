-- name: SetAutoSubtitleLibrary :exec
UPDATE libraries SET auto_subtitles=$2 WHERE id=$1 AND kind IN ('movies','series');
-- name: LockAutoSubtitleLibrary :one
SELECT * FROM libraries WHERE id=$1 FOR UPDATE;
-- name: InitializeAutoSubtitles :exec
INSERT INTO automatic_subtitles(file_id)
SELECT f.id FROM media_files f JOIN items i ON i.id=f.item_id JOIN libraries l ON l.id=i.library_id
WHERE f.available AND l.auto_subtitles AND i.kind IN ('movie','episode') ON CONFLICT DO NOTHING;
-- name: DueAutoSubtitles :many
SELECT a.file_id FROM automatic_subtitles a JOIN media_files f ON f.id=a.file_id JOIN items i ON i.id=f.item_id JOIN libraries l ON l.id=i.library_id
WHERE l.auto_subtitles AND f.available AND a.next_search_at<=now()
AND NOT EXISTS(SELECT 1 FROM jobs j WHERE j.kind='scan' AND j.resource_id=l.id AND j.state IN ('pending','running'))
AND NOT EXISTS(SELECT 1 FROM jobs j WHERE j.kind='subtitle_auto' AND j.resource_id=f.id AND j.state IN ('pending','running'))
ORDER BY a.next_search_at,a.file_id LIMIT 25;
-- name: GetAutoSubtitle :one
SELECT * FROM automatic_subtitles WHERE file_id=$1;
-- name: SetAutoSubtitleState :exec
UPDATE automatic_subtitles SET state=$2,error_code=$3,next_search_at=$4,updated_at=now() WHERE file_id=$1;
-- name: SetAutoSubtitlePublication :exec
UPDATE automatic_subtitles SET publication=$2,provider_file_id=$3,updated_at=now() WHERE file_id=$1;
-- name: ClearAutoSubtitlePublication :exec
UPDATE automatic_subtitles SET publication='{}',updated_at=now() WHERE file_id=$1;
-- name: RetryLibrarySubtitles :exec
UPDATE automatic_subtitles a SET next_search_at=now(),state='pending',error_code=''
FROM media_files f JOIN items i ON i.id=f.item_id WHERE a.file_id=f.id AND i.library_id=$1;
-- name: ListAutoSubtitles :many
SELECT a.*, i.title, i.id AS item_id, f.path,
 COALESCE((SELECT j.state FROM jobs j WHERE j.kind='subtitle_auto' AND j.resource_id=f.id AND j.state IN ('pending','running') LIMIT 1),'')::text AS job_state
FROM automatic_subtitles a JOIN media_files f ON f.id=a.file_id JOIN items i ON i.id=f.item_id
WHERE i.library_id=sqlc.arg(library_id) AND f.available
AND (sqlc.arg(after_id)::text='' OR a.file_id>sqlc.arg(after_id))
ORDER BY a.file_id LIMIT 101;
-- name: AutoSubtitleCounts :one
SELECT count(*)::bigint AS total, count(*) FILTER(WHERE a.state IS NULL OR a.state<>'available')::bigint AS missing
FROM media_files f JOIN items i ON i.id=f.item_id LEFT JOIN automatic_subtitles a ON a.file_id=f.id
WHERE i.library_id=$1 AND f.available AND i.kind IN ('movie','episode');
-- name: AutoSubtitlesWorkerReady :one
SELECT EXISTS(SELECT 1 FROM workers WHERE role='scanner' AND heartbeat_at>now()-interval '60 seconds' AND capabilities->>'autoSubtitles'='true')::boolean;
-- name: AutoSubtitleLibraryBusy :one
SELECT EXISTS(SELECT 1 FROM automatic_subtitles a JOIN media_files f ON f.id=a.file_id JOIN items i ON i.id=f.item_id
WHERE i.library_id=$1 AND a.publication<>'{}'::jsonb)::boolean;
-- name: RecoverableAutoSubtitles :many
SELECT a.file_id FROM automatic_subtitles a WHERE a.publication<>'{}'::jsonb
AND (a.state<>'recovery_error' OR a.next_search_at<=now())
AND NOT EXISTS(SELECT 1 FROM jobs j WHERE j.kind='subtitle_auto' AND j.resource_id=a.file_id AND j.state IN ('pending','running')) LIMIT 25;
-- name: GetSubtitleProviderState :one
SELECT * FROM subtitle_provider_state WHERE id=true;
-- name: SaveSubtitleProviderState :execrows
UPDATE subtitle_provider_state SET status=$1,vip=$2,remaining=$3,reset_at=$4,retry_at=$5,checked_at=now()
WHERE id=true AND lease_id=sqlc.arg(lease_id) AND lease_until>clock_timestamp();

-- name: ClaimSubtitleProviderLease :one
-- Keep the lease longer than the 30-second operation plus two 5-second cleanups.
UPDATE subtitle_provider_state SET lease_id=sqlc.arg(lease_id),lease_until=clock_timestamp()+interval '45 seconds'
WHERE id=true AND lease_until<=clock_timestamp() RETURNING lease_until;
-- name: GetSubtitleProviderLease :one
SELECT lease_id,lease_until FROM subtitle_provider_state WHERE id=true;
-- name: ReleaseSubtitleProviderLease :execrows
UPDATE subtitle_provider_state SET lease_id='',lease_until='epoch' WHERE id=true AND lease_id=sqlc.arg(lease_id);
-- name: LockAutoSubtitleJob :one
SELECT * FROM jobs WHERE id=$1 FOR UPDATE;
