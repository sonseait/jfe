# Build and operational verification

OpenSubtitles subtitle-parse diagnostics passed backend lint/four-binary builds.
Parser validation rules are unchanged; structural reasons/line numbers are now
preserved instead of replaced with a generic download error. The reported HTTP
200/text/plain failure has not been reproduced because its subtitle payload is
unavailable. No automated tests were added or run.

OpenSubtitles search now retains its typed cause in the final error-level HTTP
completion event while returning the same public 503 problem. Wait failures also
identify local-session, pool or cross-process lock contention. Validation uses
backend lint/four-binary builds; deployed contention and HTTP timeout causes have
not been observed here. No automated tests were added or run.

OpenSubtitles HTTP/operation/wait time budgets and safe failure URL diagnostics
passed backend lint and four-binary builds. No API schema changed. Actual network
stalls, proxy/DNS/TLS failures, provider lock contention and URL display in saved
job history remain unverified on a deployed server. No automated tests were run.

Moving OpenSubtitles search/download to movie/episode details passed frontend
typecheck, lint and production build. No backend contract or migration changed.
Live provider downloads, file-version switching and the remembered selection on
actual playback remain unverified. No automated browser tests were run.

Saved job history and the redesigned task dashboard use backend generation, lint
and four-binary builds plus frontend contract generation, typecheck, lint and
production build. Migration 00012 has not been applied to a live server here.
Live job logs, retry/lease-loss presentation, PostgreSQL failure behavior and
responsive UI layout remain unverified; see [task history](task-history.md).
No automated test suites were added or run.

Automated unit, integration, media fixture and browser testing was removed at the
user's request on 2026-10-05. Test files, runners, test dependencies, Makefile test
targets and CI test services/steps are no longer part of this repository.

Build/lint commands remain available:

```sh
make -C backend lint build
make -C frontend lint build
```

Generate backend contracts before frontend types:

```sh
make -C backend generate
make -C frontend generate
```

CI regenerates contracts and checks for drift, then builds and lints both apps.
A successful build is not evidence of real-device playback, GPU throughput,
provider access or worker recovery. Runtime validation, authorization, path
containment, leases and atomic publication remain product requirements.

Contextual backend logging retains build/lint validation only. No automated suites
were added or run. Actual provider HTTP responses, request/job correlation under
load and deployed log collector formatting remain unverified; example events in
[backend logging](logging.md) describe the format rather than a live diagnosis.

Automatic OpenSubtitles sidecars add contract generation, backend lint/four-binary
builds and frontend typecheck/lint/build validation only. No automated tests,
fixtures or browser suites were added or run. Migration 00011, VIP login/renewal,
provider matching/quota, writable mounts, crash recovery, permission races and
real subtitle playback require deployed-server acceptance; see
[automatic subtitle operational acceptance](automatic-subtitles.md#operational-acceptance).

The deployed HDR command has shown NVIDIA decode/encode activity. The subsequent
SDR CUDA decode/resize correction and pacing change have not yet been measured on
the user's NVIDIA host. Inspect encoder/decoder activity with `nvidia-smi dmon`
and FFmpeg fps/speed in the session's `progress.txt`. Real HDR appearance, device
compatibility and production provider behavior remain environment-dependent.

External subtitle playback now uses FFmpeg/libass and NVENC, including uploaded
SRT/WebVTT and prepared text tracks. Build/lint/typecheck do not verify rendered
appearance, timing on the NVIDIA host, PiP or cast receiver compatibility. The
subtitle editor still uses an approximate HTML timing preview.

HDR text/no-subtitle playback with a resolution ceiling now resizes CUDA frames
at their source bit depth before tone mapping. Bitmap burn-in retains its original
coordinate-preserving order. Build/lint cannot establish NVIDIA filter execution,
visual quality or whether preparing time and sustained speed improve on the host.

Player pause/close cancellation changes passed frontend typecheck, lint and build.
Browser behavior under delayed requests and native video controls remains
unverified; no automated tests were run.

Auto quality now chooses once per opened video from the browser network estimate.
Continuous quality adaptation and startup range probes were removed. Transfer
metrics no longer trigger session replacement. Actual browser network estimates
and playback continuity on the deployed server remain unverified.

Scanner subtitle fingerprint caching and direct SRT/VTT sidecar parsing have not
been measured on the deployed media volume. First-time embedded extraction still
uses FFmpeg per track. No automated tests or media fixture checks were run.

Video catalog availability filtering and scan-completion cache refresh do not
merge path identities or delete stored metadata/history. Disposable PostgreSQL
and browser checks were not run; automated tests remain disabled.

Catalog sorting contracts and sqlc bindings were regenerated. Real PostgreSQL
pagination with changing viewing state and browser interaction have not been
verified; no automated tests were run.


Chromecast integration retains build/lint/contract generation only. No automated
tests were added or run. Real Chromecast discovery, Default Media Receiver direct
play and event-HLS behavior, seeks, queue transitions, subtitles, disconnect/resume,
TLS/proxy reachability, revocation and NVENC on a deployed host remain unverified.
Successful compilation is not evidence of physical receiver compatibility.

Localized TMDB identification was inspected through live, read-only provider
requests using `Fight.Back.to.School.2.1992.BluRay.1080p.x265.10bit.2Audio.MNHD-FRDS~USLT.mkv`.
Before the correction, `vi-VN` returned ID 53165, `Trường Học Uy Long II`, with
score 15 and no recommendation; `en-US` returned the same ID with score 100.
After adding English aliases joined by provider ID, `vi-VN` retained the Vietnamese
title and returned score 100 with a recommendation. Backend lint and all four
binary builds passed. No automated suites or media fixture checks ran. A deployed
scanner rescan, database publication and poster download remain unverified.


Cast reconnect/retry changes are checked by frontend typecheck, lint and production
build only. No automated suites were added or run. Rejoining a surviving TV session,
controller synchronization, buffering recovery, paused resume, network outages,
SDK event ordering and browser background throttling remain unverified on real
Chromecast hardware; SDK cannot silently relaunch a destroyed receiver session.


Receiver application checks and direct-stream UI guidance retain frontend build,
typecheck and lint validation. Receiver video presentation and Chrome mirroring
preferences have not been verified on a physical TV. No automated tests were run.


Cast loading and toolbar corrections retain frontend typecheck/lint/build only.
Read-only requests to `https://jfe.sonlc.dev` confirmed reachable public HTTPS,
API system capabilities and a playback contract containing `target=chromecast`;
this does not verify authenticated playback or TV-side network access. Receiver
load timeout, MPEG-TS hints, direct-to-HLS fallback, toolbar presentation and actual
TV playback remain unverified on hardware. No browser or other test suites ran.

Receiver media-state corrections and MPEG-TS response MIME changes use frontend
check/build and backend lint/build validation. No automated suites were added or
run. Raw media idle/error events, status timeouts, HDMI warnings, video-output
capability checks and recovery still require real-device verification. The user's
report of “Playing on…” with a blank TV is not evidence of successful media output;
Chrome tab casting uses a separate path from Default Media Receiver URL playback.

Cast URL inspection uses frontend check/build validation only. The displayed URL
is assigned at receiver load submission, not inferred from a local-player request.
Clipboard interaction and receiver-side fetching remain unverified on hardware;
no automated suites were added or run.


Scanner indexing now defers embedded text preparation to leased scanner jobs,
uses one demux pass for missing tracks and throttles progress writes. Backend and
frontend lint/build/typecheck and ordered contract generation passed. No automated
unit, integration, media fixture or browser suites were added or run. Scan timing,
shared-volume throughput, batch extraction and cancellation/recovery have not been
measured on deployed media. Existing ffprobe/local metadata/directory costs remain.

OpenSubtitles search/download/extraction and personal subtitle publication compile,
but live provider requests were not verified with an OpenSubtitles account. Provider
keys/tokens, quotas, archive formats, scanner job completion and NVENC rendering
need deployment verification. Real libass font rendering remains unverified;
subsequent Docker dependency/font inspection is recorded below. Browser option
restoration and cast reload behavior were not exercised in a browser or on a TV.

Backend layer optimization was built with
`make -C backend docker-build REGISTRY= IMAGE_NAME=jfe-backend IMAGE_TAG=layer-size-check`
for linux/amd64. Inspection of the existing image's compressed OCI blob identified
`5be567c07b81` as the combined runtime-install layer at 230,766,529 bytes. After
splitting package groups, pruning unused Intel/AMD driver modules and unselectable
CJK serif fonts, and stripping Go debug symbols, the largest compressed layer is
64,008,125 bytes. The new image's 12 compressed filesystem layers total 263,525,804
bytes (previous image approximately 367 MB). Measurements came from Docker image
save's compressed blob sizes, not uncompressed Docker history sizes.

Manual dependency inspection in an isolated container confirmed FFmpeg/ffprobe
startup; the advertised scale_cuda, tonemap_cuda, libass subtitle filters and
H.264/HEVC NVENC encoders; MKVToolNix, Node and Python audio modules; and fontconfig
matches for all 11 selectable font families. Noto Sans CJK now resolves to the
installed SC collection, Arial to Liberation Sans, and the unprivileged user can
cache fonts through `XDG_CACHE_HOME=/cache` without cache-directory errors. No
automated suites or media fixtures ran. Real NVIDIA execution, rendered subtitle
appearance, arm64 and registry push remain unverified. A large layer can contribute
to upload timeouts, but no push failure log was available to establish causality.

The reported Harry Potter metadata job failed with `image: unknown format` during
portrait decoding. Source inspection confirmed that any portrait error previously
aborted the job before title/poster/credits publication. Live read-only TMDB lookup
for the supplied filename returned ID 673, year 2004 and score 100 in both English
and Vietnamese. The 48 available cast-image responses inspected locally were JPEG
and decoded successfully; the historical failing response is unavailable, so its
actual format/corruption cause is not established. The scanner now supports WebP
and GIF alongside JPEG/PNG and treats optional image failures as warnings, keeping
existing artwork and saving matched metadata. Remote posters are decoded/normalized
too, and poster/portrait fetching shares a 45-second deadline.

`make -C backend lint build` passed. No automated suites, fixtures or failure
injection ran. Database publication, the user's specific failing payload, deployment
and retry of job b6ef1937-db8e-42c4-b899-9ecd4bb88fbe remain unverified. After updating
scanner, rescan unmatched items or request metadata refresh to enqueue a fresh job;
the old exhausted job is not automatically reset.

Subtitle HTML-entity handling was reviewed against FFmpeg n8.0's
`libavcodec/htmlsubtitles.c` and libass 0.17.4's `ass_get_next_char`: SRT conversion
does not decode HTML entities, while libass supports escaped braces but no
literal-backslash escape. Prepared/uploaded plain-text cues now serialize directly
to ASS with escaped braces, protected literal backslashes and centisecond timing
(minimum one centisecond duration). HTML decoding remains a single import pass,
so intentionally double-escaped entities are not recursively decoded. React editor
preview remains plain text. Original sidecars/uncached embedded tracks still use
FFmpeg source decoding; their entity handling is not changed.

`make -C backend lint build` passed. No automated suites or media fixtures were
added or run. Actual libass glyph appearance, invisible-word-joiner handling across
fonts, NVENC playback and deployed sessions remain unverified.

Library scan metadata selection was reviewed in the scan API, frontend checkbox,
local metadata reader and metadata-job cascade. Ordinary scans now skip local
metadata writes for provider-identified items, enqueue missing episodes of already
identified series and restrict automatic series cascades to missing episodes.
Automatic jobs also recheck identification at execution, including older queued
jobs without a mode. Series scheduling follows completed file indexing so new
series episodes exist before metadata enumeration. Reset keeps locked metadata
and explicitly refreshes unlocked children of an identified locked series.
The checkbox now explains ordinary versus reset behavior in English/Vietnamese.

`make -C backend lint build` and `make -C frontend check build` passed. No automated
suites or fixtures ran. Database job execution, concurrent scan/reset requests,
TMDB publication and deployed scan timing remain unverified. Provider ID remains
the identification marker; local-only NFO items without an ID may still reread
local metadata. Optional missing images do not classify an identified item as
missing metadata. Explicit reset can coalesce with an already pending/running
metadata job through the existing queue deduplication; mixed concurrent requests
have not been verified.

The interrupted Cast visibility change was completed and included in frontend
checks: unavailable Cast actions remain visible and disabled, with localized
keyboard/mouse tooltips describing capability or discovery status. Actual browser
SDK loading, network discovery and receiver playback remain unverified.

Title-group entries now sort by production year ascending regardless of the
selected catalog order; unknown years sort last, with title/ID tie breakers.
Group ordering and season/episode numbering retain their existing behavior. The
catalog sort Select is widened to 210px and does not shrink under toolbar pressure;
it remains bounded by its container. `make -C frontend check build` passed. No
automated suites or browser fixtures ran; visual wrapping at actual viewport/font
sizes remains unverified.

Gemini movie recommendations were implemented with admin-only typed routes,
available-movie SQL selection, a standalone bounded REST provider client, TMDB
identity verification, owned-title exclusion and saved admin snapshots. Backend
contracts were generated through `make -C backend generate` (sqlc then OpenAPI),
followed by `make -C frontend generate`. `make -C backend lint build` and
`make -C frontend check build` passed. `git diff --check` passed. No automated
unit/integration/media/browser suites were added or run, per repository policy.
The REST request fields were checked against Google's generateContent reference:
https://ai.google.dev/api/generate-content . The default model is the documented
stable text-output `gemini-3.5-flash-lite` (July 2026); official model reference:
https://ai.google.dev/gemini-api/docs/models/gemini-3.5-flash-lite .

Live Gemini credentials/model support/quota, generated recommendation relevance,
TMDB candidate verification at runtime, database snapshot publication, browser
interaction, rate limiting under concurrent clients and reverse-proxy deadlines
remain unverified. No live Gemini request or media download was performed. The
feature requires API configuration and deployment before use. Sampling may miss
franchise context; provider identity checks do not establish franchise membership
or the accuracy of generated reasons. Unknown/misidentified catalog titles remain
a duplicate-detection limitation.

OpenSubtitles file validation errors no longer start shared provider backoff;
network/HTTP delays remain and exhausted quota still persists after an invalid
last download. `make -C backend lint build` passed. No automated tests were run.
Live provider behavior and existing persisted backoff expiry remain unverified.

A user-requested live OpenSubtitles check on 2026-10-17 loaded backend/.env
through the application config loader and called the existing provider client
directly (without the database/session gate, HTTP routes or scanner jobs). Login
and account lookup succeeded with VIP and the VIP API host. Searching Vietnamese
subtitles using `雪国列车.Snowpiercer.2013.2160p.WEB-DL.H265.HQ.60fps.DDP5.1-HHWEB.mkv`
returned 50 candidate files, including unrelated movie IDs; none passed the
existing automatic release selection for movie TMDB 110415, year 2013, without
a local movie hash. A single download of provider file 5240377 (the BluRay
DTS x264-LOST Vietnamese release) returned HTTP 200, but the existing parser
rejected the decoded 85,423-byte SRT-like payload at line 91 with
`non_positive_cue_duration` after 19 cues. This reproduces a content-validation
failure, not a timeout or authentication failure. It does not establish timing
compatibility with the user's WEB-DL/60fps video. No subtitle was published or
original media changed; the temporary diagnostic program was removed.

A subsequent user-requested live check exercised the exact `Client.Download`
method used by manual-download scanner jobs with two additional Snowpiercer
Vietnamese provider files. File 10830772 (CiNEPHiLES UHD Blu-ray Remux) succeeded
in 1,684 ms with 1,118 cues; JSON serialization for personal subtitle storage
and ASS serialization for playback also succeeded. File 4563254 (YIFY) failed
with HTTP 200 / `non_positive_cue_duration` at line 91 after 19 cues (85,127
decoded bytes), reproducing the previously supplied provider request ID
`c43b40c2-a37b-499f-93f5-cedc50298475`. This check used existing credentials
and consumed two download slots. It did not exercise UI/job creation/database
publication and made no media changes. The temporary diagnostic program was
removed; no automated test suite was added or run.

Manual download duration tolerance was verified live on 2026-10-17 using the
new `DownloadManual` method wired into manual scanner jobs. The same YIFY
provider file 4563254 that previously failed at line 91 now downloads and
parses successfully: 1,154 usable cues, 93,597 JSON bytes, valid ASS serialization,
963 ms download/parse duration. Individual non-positive-duration cues are
skipped; strict parsing remains for automatic downloads and existing uploads/
sync. `make -C backend lint build` passed. The diagnostic used the existing
provider credentials, consumed one download slot and was removed afterwards.
No automated test suite was added or run. UI/database publication and temporal
synchronization with the user's actual video remain unverified.

Provider-lock lifecycle hardening closes the underlying connection before pool
release on every operation, even after explicit unlock, to avoid retaining
reentrant session advisory locks. Timeout diagnostics use a generated sqlc
query over pg_lock_status()/pg_stat_get_activity() for the holder PID/state,
with an independent one-second best-effort budget; debug events carry the
owner PID and held duration. `make -C backend generate` and
`make -C backend lint build` passed. No automated tests were run. A temporary
read-only diagnostic could not connect to DATABASE_URL from this environment,
so live lock ownership, statistics visibility, cleanup and the cause of the
reported lock wait remain unverified. No database session was terminated.
Existing provider operations remain serialized; parallelization is not part
of this lock-lifecycle change. The diagnostic program was removed.

A user-requested read-only database investigation on 2026-10-17 successfully
connected using backend/.env and inspected job
`e579c52e-7d55-4c30-8772-8a8dbf4639d2`. The subtitle_download job had failed all
three attempts on provider_lock waits; each saved event identified PostgreSQL
PID 6395. At 17:18:32 UTC that same PID still held advisory key 7311908, was
idle/ClientRead, and had last changed state at 17:16:53 UTC. No provider jobs
were pending/running. Provider state remained connected/VIP with 996 downloads
remaining, excluding quota backoff as this job's recorded cause. The holder
application name was empty and its client connection used a local Unix socket;
these observations do not identify the owning application or why the lock
remained held. No connection was terminated and no job/state was modified.
The temporary diagnostic program was removed.

A follow-up read-only inspection at 17:21:11 UTC still found the exact same
PID 6395 with backend_start 16:55:36 UTC and state_change 17:16:53 UTC holding
key 7311908. It was a local Unix-socket client backend, idle, and its last
statement was ROLLBACK. Thus this observation establishes continued ownership
by the original session, not release followed by reacquisition. Session advisory
locks survive ROLLBACK. Six successive queries on the diagnostic connection
reported one stable server PID; transaction-pool/proxy involvement remains
unconfirmed. No backend was terminated, no lock was acquired, and the temporary
diagnostic program was removed.

Provider session locks were replaced with expiring row leases in migration
00013_subtitle_provider_lease. A single conditional UPDATE claims a unique
owner for 45 seconds using the database clock; expired owners cannot save
provider state or release a replacement lease. Cleanup releases by owner ID
and runs even if the acquisition reply was lost. No connection/transaction
is held over provider HTTP calls. All four Do callbacks now receive and use
the operation's bounded context, fixing the prior capture of outer contexts
that bypassed the advertised total 30-second deadline. Saved logs allow the
new lease ID/expiry/held duration fields and typed wait diagnostics identify
the active owner/expiry when available. `make -C backend generate` and
`make -C backend lint build` passed; no automated tests were added or run.
The new migration has not been applied to the live database and updated
services have not been deployed; crash recovery, lease reclamation and live
request behavior remain unverified. Stop old API/scanner versions, migrate
through 00013 with the external migrate CLI, then start both updated services
together. Existing advisory locks are not used by new code, but mixed old/new
versions must not run concurrently. This change hardens provider liveness
without claiming to establish the exact origin of the earlier idle session.

The OpenSubtitles detail-page modal now has a responsive maximum width of
1,100 px, a block ScrollArea content container, wrapping release details and
a minmax(0,1fr)/max-content result grid. Download buttons keep their natural
width; below 640 px they move below the details. Automatic matching now ranks
correctly identified releases across source/group differences, retains hash
priority and conflicting-ID/year/episode rejection, deduplicates provider file
IDs after ranking and attempts up to three files per job attempt. Only invalid
subtitle content advances to another file; shared provider/network errors, quota
and the total context deadline stop early. A supplemental no-hash search can
fill a short candidate list. Saved logs expose counts, file IDs and attempts.
The automatic scheduling error path preserves its daily retry time when a file
validation error has no provider-level retry time. Accepted matching policy was
updated in PLAN.md, with EN/VI library-policy help and README/architecture docs.
`make -C backend lint build` and `make -C frontend check build` passed; targeted
frontend formatting checks and `git diff --check` passed. No automated tests
were added or run. Actual modal rendering on desktop/mobile, live candidate
fallback/quota behavior, sidecar publication and synchronization of different
releases with real video remain unverified. No live provider downloads or
production database changes were performed for this change.

Subtitle search now uses shared metadata title/year query construction for
manual/default and automatic searches. Optional TMDB English-title resolution
uses a known movie/series ID, a five-second lookup context and a bounded
six-hour in-memory name cache. Known years are preserved; movies use title/year
and episodes use series title plus season/episode coordinates. Hash/source/
group/ID search constraints and video hashing were removed. Automatic matching
accepts normalized title/year agreement without requiring IDs while rejecting
known contradictions; three-file content fallback remains. Mixed Han/Latin
filename cleaning is shared with scanner/TMDB identification; original paths
and stored localized metadata are not renamed, and Han-only title hints remain
available. The manual modal displays the effective searchTitle/searchYear
returned by the API with EN/VI labels and updated automatic-policy help.
Backend contracts were generated via `make -C backend generate`, followed by
`make -C frontend generate`. Backend lint/build, frontend check/build, targeted
Prettier checks and diff whitespace checks passed. No automated test suites
were added or run.

A manual live check parsed the supplied Chinese-tracker filename as Snowpiercer
(2013), resolved a Vietnamese title hint and known TMDB 110415 to the English
Snowpiercer title, then searched OpenSubtitles by title/year without hash/ID
filters. VIP login succeeded; seven candidates were returned and all seven
passed identity/year acceptance. Ranked first were provider files 4563254,
5240377 and 5240395. No provider file was downloaded and no database state or
media was modified. A preceding attempt to inspect the live catalog file could
not complete within its context deadline, so the successful check used a known
TMDB ID/title hint rather than claiming to read that item's current metadata.
The temporary diagnostic program was removed. Scanner publication, actual UI
rendering, cache expiry and complete worker/API routing remain unverified live.

### Subtitle search fallback correction (2026-10-17)

Manual and automatic searches now prepare a cleaned filename fallback even when
localized metadata already has a title. TMDB English titles still take priority;
missing credentials/ID or lookup failure retain the Latin filename title before
the localized metadata title. Saved years remain authoritative and conflicting
filename years prevent filename fallback. Explicit user search text is preserved.
`make -C backend lint build` and `git diff --check` passed. A temporary manual
diagnostic confirmed the supplied mixed Han/Latin filename plus Vietnamese
metadata produces `query=Snowpiercer&year=2013` without a TMDB token, both with
and without a known TMDB ID; it also confirmed conflicting years retain metadata.
The diagnostic was removed. No automated suites were added or run; deployed
API/scanner behavior and modal rendering have not been verified live.
