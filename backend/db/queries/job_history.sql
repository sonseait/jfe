-- name: StartJobRun :exec
INSERT INTO job_runs(id,job_id,attempt,worker_id) VALUES($1,$2,$3,$4);

-- name: FinishJobRun :exec
UPDATE job_runs SET state=$2, finished_at=now(), dropped_logs=$3 WHERE id=$1;

-- name: AppendJobLogs :exec
INSERT INTO job_logs(run_id,event)
SELECT sqlc.arg(run_id)::text, value FROM jsonb_array_elements(sqlc.arg(events)::jsonb);

-- name: ListJobRuns :many
SELECT r.id,r.attempt,r.worker_id,r.started_at,r.finished_at,r.dropped_logs,
 CASE WHEN r.state='running' AND (j.state<>'running' OR j.lease_id<>r.id OR j.lease_until<now())
 THEN 'interrupted' ELSE r.state END::text AS state
FROM job_runs r JOIN jobs j ON j.id=r.job_id WHERE r.job_id=$1 ORDER BY r.attempt DESC;

-- name: ListJobLogs :many
SELECT l.* FROM job_logs l JOIN job_runs r ON r.id=l.run_id
WHERE r.job_id=sqlc.arg(job_id)::text AND r.id=sqlc.arg(run_id)::text AND (sqlc.arg(before_id)::bigint=0 OR l.id<sqlc.arg(before_id)::bigint)
ORDER BY l.id DESC LIMIT 201;
