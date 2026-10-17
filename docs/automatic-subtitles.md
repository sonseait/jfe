# Automatic OpenSubtitles sidecars

Scanner owns missing-subtitle searches, provider downloads, sidecar publication
and daily retries. No fifth binary or Bazarr service is introduced. API exposes
admin-only provider status/check and per-library policy, status pagination and
search-now actions through typed routes. Frontend uses generated contracts and
EN/VI strings. The existing user-scoped manual download workflow remains personal.

## Configuration and policy

Migration 00011 adds a default-off `auto_subtitles` library policy, per-file
search/publication state and one provider status row. Run the external migrate CLI
through `make -C backend migrate`; no application migration runner is involved.

Configure `OPENSUBTITLES_API_KEY`, `OPENSUBTITLES_USERNAME` and
`OPENSUBTITLES_PASSWORD` on API/scanner. A nonempty `OPENSUBTITLES_TOKEN` takes
precedence over account login. Tokens are cached only in process memory; neither
passwords nor tokens are stored in the provider status table or returned to the
frontend. Viper continues to honor explicitly empty environment overrides.
The API base returned by login is restricted to the normal/VIP OpenSubtitles API
hosts over HTTPS; signed download URLs retain provider-host/redirect restrictions.

`JFE_SUBTITLE_DOWNLOADS=true` is a deployment opt-in on API/scanner. Individual
movie/series libraries must also be enabled by an admin. Scanner needs write
permission on their parent directories; API/transcoder remain read-only. This
does not grant original subtitle editing/deletion, which remains governed by
`JFE_SUBTITLE_EDITING`. Read-only directory errors are visible in library status.

The scanner heartbeat advertises actual download preparation prerequisites.
Downloading is independent of video transcoding mode; playback capabilities are
unchanged. Admin-only provider checks run as scanner jobs. Account changes require
service restart and respect any existing provider backoff.

## Scheduling and matching

The scanner scheduler initializes state for existing/new available movie and
episode files in enabled libraries, waits for active library scans to finish,
and enqueues bounded batches of deduplicated `subtitle_auto` jobs. New indexed
files are considered at the next scheduler tick (normally within ten seconds).
Both complete and missing files are re-evaluated daily so external sidecar
changes are eventually detected. Search-now resets the next eligible time;
provider backoff remains authoritative. Expired-lease reclamation retains the
common three-attempt job limit; operation failures return to daily/provider
scheduling rather than rapidly retrying an invalid provider file.

Vietnamese language codes/names in embedded metadata or sidecar suffixes satisfy
the shared library requirement, including bitmap tracks. Personal uploads do not.
Search uses the saved metadata title and production year. When TMDB is configured
and a movie/series ID is available, an optional five-second lookup selects its
English title (`language=en-US`), cached in process for six hours (512 entries).
A failed/unconfigured lookup falls back to the cleaned Latin filename title,
then the saved title. Conflicting filename years are not used as a fallback.
Stored localized metadata and its production year are preserved. Episodes use the parent series title plus season and
episode coordinates, not an episode's localized title or the series debut year
as an episode-release-year filter. Neither hashes nor release source/group nor
provider IDs are sent as search constraints. No video content is read for search.
Mixed Han/Latin release filenames are cleaned before TMDB identification and
filename fallback: for example the supplied Snowpiercer filename yields the
Latin title Snowpiercer and year 2013. Han-only names remain available for TMDB
identification when no Latin title exists. Original paths and localized metadata
are not renamed. Metadata titles/years take precedence over filename hints.

Automatic acceptance compares complete normalized titles (including saved,
English and original-title aliases) and the saved production year; TMDB IDs are
optional corroboration, with known conflicts rejected. Candidate years can come
from provider metadata or the release filename; when the movie year is known,
a conflicting or absent candidate year is rejected. When the movie year is not
known, a matching TMDB ID is still needed. Episodes require matching series title
or parent TMDB ID and exact season/episode coordinates. Source/group/hash evidence
is not required or used for ranking. Rank prefers non-hearing-impaired subtitles,
then download count and provider file ID. Duplicate file IDs are removed.
Each search reads the first provider page, capped at 100 file candidates.
At most three ranked files are downloaded per job attempt. Invalid archive,
encoding or cue content advances to the next candidate; network/auth/rate/quota
failures stop immediately. The 30-second provider operation context and quota can
stop before three attempts. The optional TMDB lookup precedes provider-lease
acquisition and has its own five-second budget. Ordinary job retries can repeat
these attempts. Files without a future provider retry time retain the daily
scheduling delay. No match is a normal result retried the next day. There is no
automatic upgrade or measurement of alignment with the video's actual dialogue;
a different release may require timing adjustment.

A PostgreSQL row lease serializes provider operations from API and scanner.
Migration `00013_subtitle_provider_lease` adds owner/expiry fields to provider
state. Acquisition is one conditional UPDATE, expires after 45 seconds using the
database clock, and can be reclaimed after expiry. State updates require the
same live owner; release checks the owner ID so stale callers cannot overwrite
state or clear a replacement lease. Normal completion releases immediately;
process crashes or failed cleanup cannot hold the provider indefinitely.
The complete operation (including callbacks) uses a 30-second context budget;
state persistence and release each use an independent five-second cleanup budget.
No database connection or transaction spans provider HTTP calls. A process-local
gate protects the in-memory login cache. Stop old API/scanner versions before
migrating and deploying both updated services, since old advisory locks and new
leases do not coordinate with each other. Existing advisory locks are ignored by
new code; do not terminate unrelated database sessions to release them. Persisted status
contains VIP, remaining downloads, reset/retry times and sanitized error codes.
Login renewal is attempted once for authentication failures when credentials are
configured and no explicit token is supplied. Quota and HTTP Retry-After delays
also apply to manual operations; provider response bodies and signed URLs are not
logged. Unknown remaining quota is shown as unknown, not zero.
Invalid subtitle content (including archive, encoding and cue validation errors)
does not start shared provider backoff or block searches and other downloads.
An exhausted quota is still persisted even if validation of the last file fails.

## Publication and recovery

Jobs share the per-video filesystem lock with scans and original subtitle edits.
The worker verifies the file is available, inside both configured media and
library roots, and records its path/size/mtime/inode fingerprint. It checks for
existing Vietnamese sources and verifies directory writability before consuming
a download. The bounded existing ZIP/gzip/UTF-8/UTF-16 parser validates the provider
file, then plain-text cues are rendered to a size-limited UTF-8 SRT.

Staging is a hidden file in the video's directory, with one deterministic name per
video. A subsequent attempt removes abandoned staging only when there is no
publication journal and it holds the same filesystem lock. A fsynced file and
directory precede the durable database journal, which records target, staging,
video fingerprint, output fingerprint and SHA-256. Library deletion is blocked
while a journal needs reconciliation.

After committing the journal, a short publication transaction reacquires library
and file guards, locks policy/job rows and checks policy, cancellation, current
lease, roots, video fingerprint, source list and staging identity again.
`link(2)` atomically creates the complete `.vi.srt` destination without replacing
any existing file or symlink. No video/audio stream is rewritten. An application
creating the same destination wins safely; JFE never renames over it.

Recovery does not publish unpublished staging. It recognizes a proven publication
only when the target's output fingerprint and digest match the journal and the
video fingerprint still matches. Proven output is reindexed even if downloads
were disabled after publication. Outside edits are preserved. Staging is removed,
the directory is fsynced and the journal is cleared after reconciliation. Failed
recovery keeps its journal, retries after five minutes, and continues blocking
library deletion. Invalid journals/roots require operator inspection; do not
manually remove a journal without verifying its target and staging file.

Sidecar discovery retains existing path-to-track-index assignments and a persisted
index high-water mark, so a removed track's identity is not reused for new sources.
Cache/probe refresh does not clear personal timing or invalidate existing
source IDs. Active playback sessions keep their existing track list; reopening
playback obtains the new one. Disabling/removing a library never deletes sidecars.

## Operational acceptance

Build/lint/contracts are the automated checks retained by this project; no test
suites or fixtures are introduced. On a deployed server, manually verify:

- VIP login and expiration renewal, explicit-token precedence, quota reset and
  Retry-After handling, including a manual download during automatic work.
- Existing/new movies and series, absent/ambiguous matches, embedded text/bitmap
  Vietnamese tracks and sidecars, and an already existing `.vi.srt` destination.
- Read-only scanner mount, policy disable/path change during download, job
  cancellation/expired lease, an external destination appearing during download,
  worker termination before/after publication and recovery with downloads disabled.
- Library access on playback, unchanged source video, preserved old track timing,
  library deletion guard, EN/VI status pagination and NVIDIA-disabled downloads.

No live account, database migration, server filesystem recovery or device playback
is established by a successful build. Record actual deployment results separately.
