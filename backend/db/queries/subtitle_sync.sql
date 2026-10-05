-- name: CanEditSubtitles :one
SELECT EXISTS(SELECT 1 FROM users u JOIN libraries l ON l.id=sqlc.arg(library_id)
WHERE u.id=sqlc.arg(user_id) AND NOT u.disabled AND l.kind IN ('movies','series')
AND (u.role='admin' OR EXISTS(SELECT 1 FROM library_access a WHERE a.user_id=u.id AND a.library_id=l.id AND a.can_import)))::boolean;
-- name: CreateSubtitleSyncJob :exec
INSERT INTO subtitle_sync_jobs(id,user_id,file_id,track_index,action,fingerprint,offset_ms,probe_revision) VALUES($1,$2,$3,$4,$5,$6,$7,$8);
-- name: GetSubtitleSyncJob :one
SELECT * FROM subtitle_sync_jobs WHERE id=$1;
-- name: PrepareSubtitleSyncJob :exec
UPDATE subtitle_sync_jobs SET fingerprint=$2,source_path=$3,cues=$4,phase='ready' WHERE id=$1;
-- name: SetSubtitleSyncPhase :exec
UPDATE subtitle_sync_jobs SET phase=$2,error_code=$3 WHERE id=$1;
-- name: LockSubtitleFile :exec
SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg(file_id)::text, 7311904));
-- name: SubtitleFileBusy :one
SELECT EXISTS(SELECT 1 FROM subtitle_sync_jobs s JOIN jobs j ON j.id=s.id WHERE s.file_id=$1 AND s.action<>'prepare' AND (j.state IN ('pending','running') OR s.phase IN ('prepared','published')))::boolean;
-- name: SubtitlePlaybackBusy :one
SELECT EXISTS(SELECT 1 FROM playback_sessions WHERE file_id=$1 AND state IN ('ready','preparing') AND expires_at>now() AND updated_at>now()-interval '5 minutes')::boolean;
-- name: UpdateSubtitleFileProbe :exec
UPDATE media_files SET probe=$2,size=$3,modified_at=$4 WHERE id=$1;
-- name: SubtitleSyncWorkerReady :one
SELECT EXISTS(SELECT 1 FROM workers WHERE role='scanner' AND heartbeat_at>now()-interval '60 seconds' AND capabilities->>'subtitleSync'='true')::boolean;
-- name: SubtitleMKVWorkerReady :one
SELECT EXISTS(SELECT 1 FROM workers WHERE role='scanner' AND heartbeat_at>now()-interval '60 seconds' AND capabilities->>'subtitleMKV'='true')::boolean;
-- name: DeleteUploadedSubtitle :one
DELETE FROM uploaded_subtitles WHERE id=$1 AND file_id=$2 AND user_id=$3 RETURNING id;
-- name: RecoverableSubtitleJobs :many
SELECT s.* FROM subtitle_sync_jobs s JOIN jobs j ON j.id=s.id WHERE s.action<>'prepare' AND s.phase<>'abandoned' AND j.state IN ('failed','cancelled') LIMIT 100;
-- name: CompleteRecoveredSubtitleJob :exec
UPDATE jobs SET state='completed',error='',progress=100,updated_at=now() WHERE id=$1 AND state IN ('failed','cancelled');
-- name: SubtitleTranscoderBusy :one
SELECT EXISTS(SELECT 1 FROM jobs j JOIN playback_sessions p ON p.id=j.resource_id WHERE p.file_id=$1 AND j.kind='playback' AND j.state='running')::boolean;
-- name: LockSubtitleLibrary :exec
SELECT pg_advisory_xact_lock(hashtextextended(sqlc.arg(library_id)::text, 7311905));
-- name: LibrarySubtitleBusy :one
SELECT EXISTS(SELECT 1 FROM subtitle_sync_jobs s JOIN media_files f ON f.id=s.file_id JOIN items i ON i.id=f.item_id
WHERE i.library_id=$1 AND s.action<>'prepare' AND s.phase NOT IN ('indexed','abandoned'))::boolean;
