# Scheduled tasks and execution logs

Admin → Tasks separates library scan schedules from execution history. Schedule
cards show the configured interval, last scan and next due time, allow a manual
scan, and indicate automatic subtitle scheduling for enabled libraries. The next
scan is an estimate based on the existing last-scan timestamp; a healthy scanner,
queue deduplication and available worker slots determine the actual start time.
Schedule editing stays in library settings. Provider/account configuration is in
Admin → General; subtitle rendering remains in Admin → Subtitles. Per-library
subtitle consent and status stay on the padded library card.

The execution list shows the latest 100 jobs with process/status/search filters,
status counts, scan progress, attempts and a View logs action. Failures show the
latest saved error and stage instead of directing admins to worker stderr.
The modal selects individual attempts, refreshes every three seconds, and shows
200 events per page, newest first. Level filtering applies to the current page.
Older log pages pin the selected execution so a new retry cannot replace them.
Jobs predating this feature explicitly show that no logs were recorded; historical
stderr is not imported. Queued jobs have no execution until a worker claims them.

## Storage and worker lifecycle

Migration `00012_job_history` adds `job_runs` and `job_logs`. Run migrations using
the external migrate CLI before deploying API and all three workers together.
No application binary runs migrations. Generated SQL/OpenAPI/frontend types include
the change. The admin-only typed `GET /api/v1/admin/jobs/:id/history` endpoint
validates job/run IDs and a bounded numeric log cursor. Runs are scoped to their
job; arbitrary run IDs cannot fetch another job's events. Non-admin users cannot
read operational history, which may contain media titles and filenames.

Every claim gets a run identified by its lease ID, with attempt, worker and timing.
A worker creates that record before executing media work. If the record cannot
be created, execution is deferred within the normal three-attempt limit and the
job reports `job_history_unavailable`. Retries retain earlier runs. A running run
whose job lost its lease or changed ownership/state is shown as interrupted;
this does not claim that a crashed worker recorded a final event. A failed write
of the job result is distinguished from successful completion. Finishing a job
returns its ID, so a stale lease that updates zero rows cannot be reported as a
successfully saved completion.

Job-context zerolog events, including debug stage events, feed a separate bounded
PostgreSQL writer. Stderr still respects `JFE_LOG_LEVEL` and `JFE_LOG_FORMAT`;
saved job stages do not require enabling debug on the deployment. Only events
issued through the job context belong to that execution. API, scheduler and
maintenance events remain operational stderr logs, rather than being attached to
an unrelated execution.

The writer batches up to 32 events, flushes at least every 500 ms while healthy,
and has a 256-event queue. At most 2,000 ordinary events are accepted per run;
final completion/failure/cancellation events have separate capacity. Media work
and lease renewal never wait on log inserts. Individual writes have two-second
timeouts. Normal completion drains the queue for at most five seconds, then
attempts to save the final events separately. A process crash can lose the
unflushed tail. Queue overflow, the cap and persistence failures increment an
omitted-event count shown in the modal; database failures also go to stderr.
Logs are retained with their jobs and cascade on job deletion. There is no
automatic age-based deletion in this change, so storage grows with executions.

Persisted events use an explicit field allowlist, bounded text, configured-secret
redaction and URL/credential redaction. The vetted `provider.url` diagnostic is
preserved for public OpenSubtitles API routes and allowlisted search parameters;
signed download paths and queries remain redacted. Request bodies, job payloads, command
arguments and raw provider responses are excluded. Provider HTTP/network/JSON
classification is retained, with signed download paths excluded as described in
[logging](logging.md). Error summaries in the job list use these saved, sanitized
events rather than raw errors from database models.

## Verification limits

Validation uses backend generation/lint/four-binary builds and frontend contract
generation/typecheck/lint/production build. No automated test suites are added or
run. Migration execution, live polling during long jobs, database outage/queue
saturation, worker crash recovery, provider failures and layout on actual devices
still require operational verification on the deployment.
