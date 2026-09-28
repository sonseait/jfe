-- name: SaveSubtitle :one
INSERT INTO uploaded_subtitles(id,file_id,user_id,name,cues) VALUES($1,$2,$3,$4,$5) RETURNING id,name,jsonb_array_length(cues)::int AS cue_count;
-- name: ListSubtitles :many
SELECT id,name,jsonb_array_length(cues)::int AS cue_count FROM uploaded_subtitles WHERE file_id=$1 AND user_id=$2 ORDER BY created_at,id;
-- name: GetSubtitle :one
SELECT * FROM uploaded_subtitles WHERE id=$1 AND file_id=$2 AND user_id=$3;
