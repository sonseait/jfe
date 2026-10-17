CREATE TABLE job_runs (
 id text PRIMARY KEY,
 job_id text NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
 attempt integer NOT NULL,
 worker_id text NOT NULL,
 started_at timestamptz NOT NULL DEFAULT now(),
 finished_at timestamptz,
 state text NOT NULL DEFAULT 'running',
 dropped_logs integer NOT NULL DEFAULT 0,
 UNIQUE (job_id, attempt)
);
CREATE TABLE job_logs (
 id bigserial PRIMARY KEY,
 run_id text NOT NULL REFERENCES job_runs(id) ON DELETE CASCADE,
 event jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX job_logs_run_idx ON job_logs(run_id, id);

CREATE INDEX job_logs_errors_idx ON job_logs(run_id, id DESC) WHERE event ? 'error';

CREATE INDEX jobs_history_order ON jobs(created_at DESC, id DESC);
