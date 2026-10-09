-- name: CountUsers :one
SELECT count(*) FROM users;
-- name: LockSetup :exec
SELECT pg_advisory_xact_lock(7311901);
-- name: CreateUser :one
INSERT INTO users(id,username,password_hash,role) VALUES($1,$2,$3,$4) RETURNING *;
-- name: UserByName :one
SELECT * FROM users WHERE username=$1;
-- name: GetUser :one
SELECT * FROM users WHERE id=$1;
-- name: ListUsers :many
SELECT * FROM users ORDER BY username;
-- name: UpdateUser :one
UPDATE users SET username=$2,role=$3,disabled=$4 WHERE id=$1 RETURNING *;
-- name: UpdatePassword :exec
UPDATE users SET password_hash=$2 WHERE id=$1;
-- name: DeleteUser :exec
DELETE FROM users WHERE id=$1;
-- name: InsertSession :exec
INSERT INTO sessions(token_hash,user_id,expires_at) VALUES($1,$2,$3);
-- name: Authenticate :one
SELECT u.* FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=$1 AND s.expires_at>now() AND NOT u.disabled;
-- name: RevokeSession :exec
DELETE FROM sessions WHERE token_hash=$1;
-- name: RevokeUserSessions :exec
DELETE FROM sessions WHERE user_id=$1;
-- name: ListLibraries :many
SELECT l.* FROM libraries l WHERE sqlc.arg(is_admin)::boolean OR EXISTS(SELECT 1 FROM library_access a WHERE a.library_id=l.id AND a.user_id=sqlc.arg(user_id)::text) ORDER BY name;
-- name: GetLibrary :one
SELECT * FROM libraries WHERE id=$1;
-- name: SaveLibrary :one
INSERT INTO libraries(id,name,kind,paths,scan_interval_hours) VALUES($1,$2,$3,$4,$5) ON CONFLICT(id) DO UPDATE SET name=excluded.name,paths=excluded.paths,scan_interval_hours=excluded.scan_interval_hours RETURNING *;
-- name: DeleteLibrary :exec
DELETE FROM libraries WHERE id=$1;
-- name: GrantLibrary :exec
INSERT INTO library_access(user_id,library_id) VALUES($1,$2) ON CONFLICT DO NOTHING;
-- name: ClearAccess :exec
DELETE FROM library_access WHERE user_id=$1;
-- name: UserLibraries :many
SELECT library_id FROM library_access WHERE user_id=$1;
-- name: CanAccess :one
SELECT EXISTS(SELECT 1 FROM libraries l WHERE l.id=$1 AND ($3::boolean OR EXISTS(SELECT 1 FROM library_access a WHERE a.library_id=l.id AND a.user_id=$2)))::boolean;
-- name: DueLibraries :many
SELECT * FROM libraries WHERE scan_interval_hours>0 AND last_scan_at+scan_interval_hours*interval '1 hour'<now();
-- name: ScannedLibrary :exec
UPDATE libraries SET last_scan_at=now() WHERE id=$1;
-- name: DeleteEmptySeries :exec
DELETE FROM items i WHERE i.library_id=$1 AND i.kind='series'
AND NOT EXISTS (SELECT 1 FROM items child WHERE child.parent_id=i.id);
-- name: UpsertItem :exec
INSERT INTO items(id,library_id,parent_id,kind,title,sort_title,year,season,episode) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)
ON CONFLICT(id) DO UPDATE SET parent_id=excluded.parent_id,season=excluded.season,episode=excluded.episode;
-- name: SaveFile :exec
INSERT INTO media_files(id,item_id,path,size,modified_at,duration,probe) VALUES($1,$2,$3,$4,$5,$6,$7)
ON CONFLICT(item_id,path) DO UPDATE SET item_id=excluded.item_id,size=excluded.size,modified_at=excluded.modified_at,duration=excluded.duration,probe=excluded.probe,available=true;
-- name: GetItem :one
SELECT * FROM items WHERE id=$1;
-- name: SeriesEpisodes :many
SELECT * FROM items WHERE parent_id=$1 AND kind='episode' ORDER BY season,episode,sort_title,id;
-- name: CatalogEpisodes :many
SELECT i.* FROM items i WHERE i.parent_id=sqlc.arg(parent_id)::text AND i.kind='episode'
AND EXISTS (SELECT 1 FROM media_files f WHERE f.item_id=i.id AND f.available)
AND (sqlc.arg(is_admin)::boolean OR EXISTS(SELECT 1 FROM library_access a WHERE a.library_id=i.library_id AND a.user_id=sqlc.arg(user_id)::text))
ORDER BY i.season,i.episode,i.sort_title,i.id;
-- name: ListItems :many
SELECT i.* FROM items i LEFT JOIN user_state resume_state ON resume_state.item_id=i.id AND resume_state.user_id=sqlc.arg(user_id)::text
CROSS JOIN LATERAL (SELECT
  CASE WHEN NOT sqlc.arg(resume)::boolean AND sqlc.arg(sort)::text='watching'
    AND resume_state.position>0 AND NOT resume_state.watched THEN 1 ELSE 0 END AS watching_rank,
  CASE WHEN sqlc.arg(resume)::boolean THEN COALESCE(resume_state.updated_at,'epoch'::timestamptz)
    WHEN sqlc.arg(sort)::text='newest' THEN i.created_at
    WHEN sqlc.arg(sort)::text='watching' AND resume_state.position>0 AND NOT resume_state.watched THEN resume_state.updated_at
    ELSE 'epoch'::timestamptz END AS sort_time
) sort_keys WHERE
(i.kind NOT IN ('movie','episode','series')
 OR EXISTS (SELECT 1 FROM media_files f WHERE f.item_id=i.id AND f.available)
 OR (i.kind='series' AND EXISTS (
   SELECT 1 FROM items child JOIN media_files f ON f.item_id=child.id
   WHERE child.parent_id=i.id AND child.library_id=i.library_id AND f.available
 )))
AND
(sqlc.arg(is_admin)::boolean OR EXISTS(SELECT 1 FROM library_access a WHERE a.library_id=i.library_id AND a.user_id=sqlc.arg(user_id)::text))
AND (sqlc.arg(library_id)::text='' OR i.library_id=sqlc.arg(library_id))
AND (sqlc.arg(parent_id)::text='' OR i.parent_id=sqlc.arg(parent_id))
AND (sqlc.arg(resume)::boolean OR sqlc.arg(library_id)::text='' OR sqlc.arg(parent_id)::text<>'' OR sqlc.arg(kind)::text<>'' OR i.parent_id='')
AND (sqlc.arg(kind)::text='' OR i.kind=sqlc.arg(kind))
AND (NOT sqlc.arg(top_level)::boolean OR i.parent_id='' OR (sqlc.arg(search)::text<>'' AND i.kind IN ('track','podcast_episode','book_part')))
AND (sqlc.arg(artist)::text='' OR EXISTS(SELECT 1 FROM audio_metadata a JOIN items child ON child.id=a.item_id WHERE (child.id=i.id OR child.parent_id=i.id) AND a.tags->'artists' ? sqlc.arg(artist)))
AND (sqlc.arg(person_id)::text='' OR i.cast_members @> jsonb_build_array(jsonb_build_object('id',sqlc.arg(person_id)::text)))
AND (sqlc.arg(search)::text='' OR i.title ILIKE '%'||sqlc.arg(search)||'%' OR EXISTS(SELECT 1 FROM audio_metadata a WHERE a.item_id=i.id AND a.tags->>'artists' ILIKE '%'||sqlc.arg(search)||'%'))
AND (NOT sqlc.arg(favorites)::boolean OR EXISTS(SELECT 1 FROM user_state s WHERE s.user_id=sqlc.arg(user_id) AND s.item_id=i.id AND s.favorite))
AND (NOT sqlc.arg(resume)::boolean OR EXISTS(SELECT 1 FROM user_state s WHERE s.user_id=sqlc.arg(user_id) AND s.item_id=i.id AND s.position>0 AND NOT s.watched))
AND (sqlc.arg(after_id)::text=''
 OR sort_keys.watching_rank < sqlc.arg(after_watching)::integer
 OR (sort_keys.watching_rank = sqlc.arg(after_watching)::integer AND (
   sort_keys.sort_time < COALESCE(NULLIF(sqlc.arg(after_updated_at)::text,'')::timestamptz,'epoch'::timestamptz)
   OR (sort_keys.sort_time = COALESCE(NULLIF(sqlc.arg(after_updated_at)::text,'')::timestamptz,'epoch'::timestamptz)
       AND (i.sort_title,i.id)>(sqlc.arg(after_title)::text,sqlc.arg(after_id)::text))
 )))
ORDER BY sort_keys.watching_rank DESC,sort_keys.sort_time DESC,i.sort_title,i.id LIMIT sqlc.arg(page_limit);
-- name: ItemFiles :many
SELECT * FROM media_files WHERE item_id=$1 ORDER BY path;
-- name: GetFile :one
SELECT * FROM media_files WHERE id=$1;
-- name: FileByPath :one
SELECT f.* FROM media_files f JOIN items i ON i.id=f.item_id WHERE f.path=$1 AND i.library_id=$2;
-- name: UnavailableFile :exec
UPDATE media_files SET available=false WHERE id=$1;
-- name: LibraryFiles :many
SELECT f.* FROM media_files f JOIN items i ON i.id=f.item_id WHERE i.library_id=$1;
-- name: SaveMetadata :one
UPDATE items SET title=$2,sort_title=lower($2),year=$3,overview=$4,poster=$5,provider_id=$6,metadata_locked=$7,cast_members=$8 WHERE id=$1 RETURNING *;
-- name: GetState :one
SELECT * FROM user_state WHERE user_id=$1 AND item_id=$2;
-- name: SetState :one
INSERT INTO user_state(user_id,item_id,favorite,watched) VALUES($1,$2,$3,$4) ON CONFLICT(user_id,item_id) DO UPDATE SET favorite=excluded.favorite,watched=excluded.watched,updated_at=now() RETURNING *;
-- name: SaveProgress :exec
INSERT INTO user_state(user_id,item_id,position,watched) VALUES($1,$2,$3,$4) ON CONFLICT(user_id,item_id) DO UPDATE SET position=excluded.position,watched=excluded.watched,updated_at=now();
-- name: Enqueue :one
INSERT INTO jobs(id,role,kind,resource_id,payload) VALUES($1,$2,$3,$4,$5) ON CONFLICT(kind,resource_id) WHERE state IN ('pending','running') DO UPDATE SET resource_id=excluded.resource_id RETURNING *;
-- name: ClaimJob :one
WITH candidate AS (SELECT j.id FROM jobs j WHERE j.role=$1 AND next_attempt_at<=now() AND NOT cancel_requested AND attempts<3 AND (state='pending' OR (state='running' AND lease_until<now() AND j.role IN ('scanner','downloader'))) ORDER BY CASE WHEN j.kind='subtitle_prepare' THEN 1 ELSE 0 END,j.created_at FOR UPDATE SKIP LOCKED LIMIT 1)
UPDATE jobs SET state='running',progress=0,total_files=0,processed_files=0,attempts=attempts+1,lease_id=$2,lease_until=now()+interval '30 seconds',updated_at=now() FROM candidate WHERE jobs.id=candidate.id RETURNING jobs.*;
-- name: RenewJob :one
UPDATE jobs SET lease_until=now()+interval '30 seconds',updated_at=now() WHERE id=$1 AND lease_id=$2 AND state='running' AND NOT cancel_requested RETURNING id;
-- name: FinishJob :exec
UPDATE jobs SET state=$3,error=$4,next_attempt_at=CASE WHEN $3='pending' THEN now()+least(300,10*attempts*attempts)*interval '1 second' ELSE next_attempt_at END,progress=CASE WHEN $3='completed' THEN 100 ELSE progress END,updated_at=now() WHERE id=$1 AND lease_id=$2 AND state='running';
-- name: CancelJob :exec
UPDATE jobs SET cancel_requested=true,state=CASE WHEN state='pending' THEN 'cancelled' ELSE state END WHERE id=$1 AND state IN ('pending','running');
-- name: ListJobs :many
SELECT * FROM jobs ORDER BY created_at DESC LIMIT 100;
-- name: ListJobDetails :many
SELECT sqlc.embed(j), COALESCE(l.name, i.title, pi.title, si.title, '')::text AS resource_name,
 COALESCE(i.id, pi.id, si.id, '')::text AS item_id,
 COALESCE(l.id, i.library_id, pi.library_id, si.library_id, '')::text AS library_id
FROM jobs j
LEFT JOIN libraries l ON j.kind='scan' AND l.id=j.resource_id
LEFT JOIN items i ON j.kind='metadata' AND i.id=j.resource_id
LEFT JOIN playback_sessions p ON j.kind='playback' AND p.id=j.resource_id
LEFT JOIN items pi ON pi.id=p.item_id
LEFT JOIN media_files sf ON (j.kind IN ('subtitle_sync','subtitle_prepare') AND sf.id=j.resource_id)
 OR (j.kind='subtitle_download' AND sf.id=j.payload->>'fileId')
LEFT JOIN items si ON si.id=sf.item_id
ORDER BY j.created_at DESC LIMIT 100;
-- name: Heartbeat :exec
INSERT INTO workers(id,role) VALUES($1,$2) ON CONFLICT(id) DO UPDATE SET heartbeat_at=now();
-- name: ListWorkers :many
SELECT * FROM workers WHERE heartbeat_at>now()-interval '60 seconds' ORDER BY role;
-- name: ReapJobs :exec
UPDATE jobs SET state=CASE WHEN cancel_requested THEN 'cancelled' ELSE 'failed' END,error='Worker lease expired',updated_at=now() WHERE state='running' AND lease_until<now() AND (cancel_requested OR role='transcoder' OR attempts>=3);
-- name: StartPlayback :exec
INSERT INTO playback_sessions(id,user_id,item_id,file_id,token_hash,method,state,start_position,position,expires_at,decision,preview) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$8,$9,COALESCE(sqlc.narg('decision')::jsonb,'{}'::jsonb),sqlc.arg(preview));
-- name: GetPlayback :one
SELECT * FROM playback_sessions WHERE id=$1;
-- name: SetPlaybackReady :exec
UPDATE playback_sessions SET state='ready',updated_at=now() WHERE id=$1 AND state='preparing';
-- name: FailPlayback :exec
UPDATE playback_sessions SET state='failed',updated_at=now() WHERE id=$1 AND state IN ('preparing','ready');
-- name: StopPlayback :exec
UPDATE playback_sessions SET state='stopped',updated_at=now() WHERE id=$1 AND user_id=$2;
-- name: PlaybackProgress :one
UPDATE playback_sessions SET position=$3,sequence=$4,updated_at=now() WHERE id=$1 AND user_id=$2 AND sequence<$4 AND state IN ('ready','preparing') AND expires_at>now() RETURNING *;
-- name: CancelPlaybackJobs :exec
UPDATE jobs SET cancel_requested=true WHERE resource_id=$1 AND role='transcoder' AND state IN ('running','pending');
-- name: ExpirePlayback :exec
UPDATE playback_sessions SET state='stopped' WHERE state IN ('ready','preparing') AND (expires_at<now() OR updated_at<now()-interval '5 minutes');
-- name: GetSetting :one
SELECT value FROM settings WHERE key=$1;
-- name: SaveSetting :exec
INSERT INTO settings(key,value) VALUES($1,$2) ON CONFLICT(key) DO UPDATE SET value=excluded.value;
-- name: PatchSetting :exec
INSERT INTO settings(key,value) VALUES($1,$2) ON CONFLICT(key) DO UPDATE SET value=settings.value || excluded.value;

-- name: LockTranscodes :exec
SELECT pg_advisory_xact_lock(7311902);
-- name: ActiveTranscodes :one
SELECT count(*) FROM jobs WHERE role='transcoder' AND state='running' AND lease_until>now();
-- name: ReapPlayback :exec
UPDATE playback_sessions SET state='failed' WHERE state IN ('preparing','ready') AND EXISTS(SELECT 1 FROM jobs j WHERE j.resource_id=playback_sessions.id AND j.role='transcoder' AND j.state IN ('failed','cancelled'));

-- name: LibraryFileCount :one
SELECT count(*) FROM media_files f JOIN items i ON i.id=f.item_id WHERE i.library_id=$1 AND f.available;
-- name: LatestLibraryScan :one
SELECT * FROM jobs WHERE kind='scan' AND resource_id=$1 ORDER BY created_at DESC LIMIT 1;
-- name: ReportScan :one
UPDATE jobs SET total_files=$3,processed_files=$4,progress=$5,updated_at=now()
WHERE id=$1 AND lease_id=$2 AND state='running' AND NOT cancel_requested AND lease_until>now() RETURNING id;
