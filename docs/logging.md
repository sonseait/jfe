# Backend operational logs

All four binaries write structured zerolog events to stderr. Every event after
bootstrap identifies `service` (`api`, `scanner`, `transcoder`, `downloader`) and
`pid`. Worker events add `workerRole` and `workerId`. Configuration/dependency
startup, PostgreSQL connection duration/pool size and worker capabilities are
logged without dumping environment values or connection strings.

Use `JFE_LOG_LEVEL=info` for normal operation, `debug` for investigation, and
`JFE_LOG_FORMAT=json` for log collection (`console` is available for local use).
Restart affected binaries after changing these settings.

## Requests and jobs

API completion events carry `requestId`, method, route template, operation ID,
HTTP status and `durationMs`. Authenticated typed handlers add `userId`; validated
UUID path parameters are included. Bodies, query strings, authorization headers,
stream tokens and raw request URLs are excluded. Internal errors stay private in
HTTP responses. One completion event reports the underlying server error; expected
4xx responses use warn, 5xx use error. Successful health/status/job polling,
playback progress and stream-segment requests use debug rather than filling info.

Common API enqueue events associate `requestId` with `jobId`. Follow `jobId` across
worker events; playback additionally uses `playbackId`. This is correlation through
events, not a persisted distributed tracing system. Jobs created by background
schedulers have no originating HTTP request. Resource context is resolved from
current records on a best-effort basis, so deleted/missing resources may have IDs
without a human-readable name.

Jobs log start, completion, cancellation and failure with kind, resource ID,
attempt, queue wait and execution duration. Where available, events include
library ID/name, item ID/title, media file ID/filename, import source or playback
ID. No raw job payload is logged. Failure includes the last processing `stage`,
`state`, `willRetry` and job backoff (`retryInMs`) when applicable. A failed result
write is reported separately, so execution success is not silently treated as a
persisted completion. Lease renewal failures are warnings followed by cancellation.

At info, scans report progress at most every 15 seconds plus start/final counts,
including processed/total/skipped files. Individual stage transitions use debug;
warnings identify skipped filenames. Metadata, audio tags, subtitle work, YouTube
downloads and playback have named stages, which appear on their final job event.
Playback records its direct/remux/transcode decision and reason, FFmpeg PID and
effective output constraints, then logs when the stream becomes ready. Full
FFmpeg argument lists and media URLs are not added to logs.

## OpenSubtitles time budgets

Each API HTTP call has a 10-second timeout; downloading the subtitle file has a
15-second timeout, both including body reads. The shared transport caps TCP
connection establishment and TLS handshake at three seconds each, and response
headers at five seconds. A provider operation (including login/account refresh,
retry and download) has a 30-second context budget. Waiting for the in-process
gate and cross-process provider lease acquisition share a separate
five-second budget. Timeout/cancellation interrupts the local wait as well as SQL
lease polling. Bookkeeping and lease release use their existing independent,
bounded cleanup contexts, so the complete handler can outlive the operation's
network budget during database trouble.

A network timeout has `reason=request_timeout`; a provider wait that expires has
`reason=provider_wait_timeout` and `requestSkipped=true`, since no outbound request
was sent. `waitStage` distinguishes `local_session` (the in-process provider
gate) and `provider_lease` (the cross-process expiring row lease, including its
database pool/query wait). Older events used `database_pool`/`provider_lock`.
Search failures preserve the underlying typed cause when converted to a public
503, so the final error-level HTTP completion event includes these diagnostics
and the failed HTTP URL when a request was actually sent. No separate warning
event is required to recover the original provider cause.
An operation deadline outside a specific HTTP call is
`reason=operation_timeout`. `timeoutMs` is the request budget (or a shorter parent
deadline); transport phases can time out earlier. Backoff and caller cancellations
retain their existing behavior. No URL is invented for requests that were skipped.

## Saved execution history

Job-context events are also saved per execution/attempt in PostgreSQL and shown
in Admin → Tasks. Saved stage events are independent of the stderr log level.
The modal polls and paginates by execution; previous attempts survive retries.
Storage is bounded per run and reports omitted events. No historical stderr is
imported. See [task history](task-history.md) for migration and failure behavior.

## Error details

OpenSubtitles errors retain their public error code but include a nested
`provider` diagnostic object: method, API endpoint (without query), host, HTTP
status/text, content type, safe request ID, reason, bounded/redacted provider
message and retry/reset time when available. Failed HTTP calls also include
`provider.url`, `durationMs` and `timeoutMs`. API URLs retain allowlisted public
search parameters so the request can be reproduced; credentials and unknown
parameters are excluded. Signed download URLs retain the HTTPS host only, with
`/[redacted]` as their path and `urlRedacted=true`. These vetted URLs are also
preserved in saved job logs. Requests still require the appropriate API-key/auth
headers when reproduced; headers and request bodies are never logged. DNS, TLS, network timeout and JSON
type/syntax failures have distinct reasons. Download-host paths are represented
by `subtitle_file`; signed URLs are never logged. Known credentials, URLs,
authorization fragments and opaque secret-like values are removed from the
provider message. Only the response's `message` field is considered, not its raw
body. A provider backoff event has `requestSkipped=true`: no new request was made,
so find the preceding failure for the original HTTP status. After restart only
the persisted code/backoff may be available.
File validation errors (`subtitle_file` / `invalid_subtitle_response`) have no
`retryAt` and do not trigger shared backoff. Network/HTTP failures retain their
provider delays. Existing persisted backoff expires normally after upgrading.

On `waitStage=provider_lease` timeouts, a best-effort lookup with its own one-second
budget adds `leaseOwner` and `leaseUntil`. A missing expiry or owner means the
lookup could not identify an active owner. No SQL text or connection credentials
are recorded. Acquisition/release debug events carry `providerLeaseId`,
acquisition `providerLeaseUntil`, and operation completion `heldMs`, correlated
with the request/job context and preserved in saved job logs. Release failures
log a warning; the lease expires automatically after 45 seconds. A state write
rejected because ownership expired/changed uses `reason=provider_lease_lost`.
Historical session-lock events retain `lockHolderPid` and PostgreSQL state;
updated provider code no longer acquires those session locks.

Subtitle payload parse failures retain `reason=invalid_subtitle_response` and
include the original structural parser reason in `detail`, plus `parserReason`,
one-based `parserLine` when applicable, `parsedCues`, a sanitized filename,
decoded `responseBytes` and an inferred `responseFormat`. The format is a hint
from bounded content inspection (for example SRT/WebVTT, ASS/SSA, HTML or JSON),
not proof of a valid subtitle document. HTTP 200 and text/plain alone do not
establish successful subtitle parsing. No subtitle text or body sample is logged.
Automatic download parsing remains strict. Manually selected downloads skip
individual zero/negative-duration cues; unsupported formats still fail.

PostgreSQL failures add SQLSTATE, table and constraint names without SQL arguments
or database error detail values. Filesystem errors identify operation/filename;
command failures retain exit codes. YouTube errors expose existing sanitized
provider categories. Scheduler, capability publication, job reaping and playback
expiry errors are no longer silently discarded. Selected recovery failures also
identify the affected job/file; cleanup/recovery retains its existing behavior.

For example, an HTTP failure event can contain the following fields (illustrative,
not a diagnosis of a particular deployed provider failure):

```json
{
  "level": "error",
  "service": "scanner",
  "jobId": "4311a1ae-f152-4b8a-99ea-687cbeec5119",
  "kind": "subtitle_provider_check",
  "attempt": 1,
  "stage": "subtitle_provider_check",
  "provider": {
    "name": "opensubtitles",
    "code": "provider_error",
    "method": "GET",
    "endpoint": "/api/v1/infos/user",
    "httpStatus": 502,
    "httpStatusText": "Bad Gateway",
    "reason": "http_error"
  },
  "willRetry": true,
  "retryInMs": 10000,
  "message": "job failed"
}
```

Example queries against collected JSON logs:

```sh
jq 'select(.jobId == "4311a1ae-f152-4b8a-99ea-687cbeec5119")' backend.log
jq 'select(.level == "error" or .level == "warn")' backend.log
jq 'select(.playbackId != null)' backend.log
```

Validation is limited to build/lint and contract generation. No automated test
suites or live provider/server acceptance runs are implied by these examples.

Automatic subtitle search logs include `searchResults`, `matchedCandidates` and
`candidateLimit`. Each download records `providerFileId` and `candidateAttempt`;
file-content rejection records the typed provider diagnostic before trying the
next ranked candidate. These fields are retained in per-job execution history.

Metadata-based subtitle searches record `subtitleSearchTitle` and
`subtitleSearchYear` (also in saved job history). Optional TMDB English-title
lookup failures use `reason=tmdb_english_title_lookup_failed`, with HTTP status
or timeout/cancellation classification when available; raw provider bodies,
credentials and signed URLs remain excluded. The lookup falls back to saved
metadata and runs outside the OpenSubtitles lease.
