-- name: ListPersonalSubtitleTiming :many
SELECT source_key,offset_ms FROM personal_subtitle_timing WHERE user_id=$1 AND file_id=$2;

-- name: SavePersonalSubtitleTiming :exec
INSERT INTO personal_subtitle_timing(user_id,file_id,source_key,offset_ms) VALUES($1,$2,$3,$4)
ON CONFLICT(user_id,file_id,source_key) DO UPDATE SET offset_ms=EXCLUDED.offset_ms;

-- name: DeletePersonalSubtitleTiming :exec
DELETE FROM personal_subtitle_timing WHERE user_id=$1 AND file_id=$2 AND source_key=$3;

-- name: ClearOriginalSubtitleTiming :exec
DELETE FROM personal_subtitle_timing WHERE file_id=$1 AND source_key LIKE 'track:%';
