# Native backend validation

## Verified locally

- Go unit tests: typed route binding, validation, authorization metadata,
  OpenAPI generation, duplicate registration, media naming and path containment.
  Fiber method wrappers cover required/optional fields, enum/array constraints,
  exact integer bounds, binary responses and equivalent parameterized paths.
  Viper tests cover .env loading, process environment precedence, missing files
  and sanitized parse errors.
- PostgreSQL/FFmpeg integration: disposable schema migrations, first-admin setup,
  users/library access, scanner ingestion, HTTP Range 206, ordered progress,
  stopped playback token denial, sidecar discovery on rescan and real remux HLS.
  Disabled encoding returns 409; CPU mode returns 422; NVIDIA settings round-trip. Run `make -C backend test-integration` with JFE_TEST_DATABASE_URL set.
  Plain `go test` skips database suites unless the script supplies a prepared schema.
- Official migrate CLI integration: fresh up, repeated up, dirty-state rejection,
  upgrading legacy CPU settings to disabled while preserving quality/concurrency,
  and down/up cycle in backend/scripts/test-integration.sh. The script prepares
  isolated schemas before running API/worker tests; browser E2E also uses the CLI.
- Frontend TypeScript, ESLint and production build. Native query tests cover
  detail navigation, account cache isolation and logout credential cleanup.
- Native Playwright desktop/mobile Chromium: real API and worker processes,
  setup/login, library creation, scan, catalog/detail, favorite, direct playback,
  mini-player fullscreen, navigation during playback, stop/resume, admin and language.
  Scan file totals and completion progress are checked on library cards;
  disabled quality/subtitle controls are checked on desktop/mobile Chromium.
  An injected direct-play browser error verifies remux recovery without encoding.
  Admin checks cover the two modes and saving/reloading NVIDIA configuration.
  Library archive and grouped catalog layouts are captured at desktop/mobile sizes;
  browser checks cover toggling grouping, collapsing and expanding groups.
  Discover desktop/mobile screenshots cover the spotlight and poster-free layout;
  E2E verifies detail navigation and starting playback from the resume shelf.
  Administration library tests cover name filtering, editing the scan interval,
  saving existing libraries and modal horizontal overflow with long media paths;
  screenshots cover the list and editor on desktop/mobile.
- Title grouping tests cover sequels, shared prefixes, Vietnamese normalization,
  numeric sorting, library/kind isolation and extending a group across pages.
- Metadata ranking tests cover filename cleanup, remake years, missing years,
  original titles, series and ambiguous candidates. PostgreSQL worker tests use
  a simulated TMDB transport to verify applying the correct remake and preserving
  metadata when ambiguous, without retrying an identification that needs input.
  Live TMDB account behavior and match accuracy on a real library remain unverified.
- Task API tests verify resource title/item/library links and administrator-only
  access. Desktop/mobile browser flows exercise manual metadata saving, user
  creation/search and task context. Candidate preselection/manual override uses
  isolated API fixtures in the browser test only.
- PostgreSQL job tests: concurrent claims do not overlap, active leases cannot
  be reclaimed, expired scanner leases retry, stale completion is fenced,
  scan counters reset on reclaim and stale progress is rejected,
  cancelled crashed scanners are reaped, expired transcoding jobs fail and
  configured encoding concurrency is enforced.
- Real FFmpeg resolution tests cover 720p, 1080p, 4K, portrait aspect ratios,
  even output dimensions and no upscaling.
- Uploaded subtitle tests cover UTF-8 SRT/VTT, timestamps, multiline text,
  markup removal, invalid/oversize files and per-account/library access.
  PostgreSQL integration verifies upload, listing and retrieval; browser checks
  cover uploading, immediate timing changes and a retained frame during HLS seek.
  Actual NVENC burn-in timing (especially bitmap tracks) still needs GPU testing.
- Docker Linux amd64 image builds; FFmpeg exposes h264_nvenc and libass subtitles.
  Runtime uses Debian/glibc for NVIDIA driver library compatibility.
- Worker integration checks reject queued encoding after disabling it and simulate
  an NVENC driver failure: one NVENC attempt, failed session, cache cleanup and
  no CPU retry. This simulation does not validate actual GPU operation.

The CI workflow regenerates sqlc, OpenAPI and TypeScript artifacts and checks
for drift, then runs Go/frontend checks and native browser tests. The workflow
has been added but has not yet run on a remote CI provider.

## Remaining acceptance work

This is a working first implementation, not completion of every PLAN.md gate.

- Retry exhaustion, actual worker process crash/restart and cache cleanup
  regression tests (lease transitions are covered separately).
- Large native catalogs: infinite-scroll retry/filter reset and cursor behavior
  under concurrent library changes. Catalog currently sorts by title only.
- Multiple media versions, varied naming conventions and numerical episode
  ordering across seasons; large multi-root libraries.
- Real TMDB account/provider responses, metadata match accuracy and artwork.
- Actual NVIDIA GPU execution is not verified on the local macOS host. Run
  JFE_TEST_NVENC=1 with the integration suite on an NVIDIA host; successful
  hardware HLS, driver compatibility and subtitle burn-in need device validation.
- Audio switching and bitmap/ASS subtitle timing on seek/resume; HDR sources,
  uncommon codecs, long-running HLS and FFmpeg resource limits.
- Real Safari/iOS and Firefox, native HLS, touch fullscreen, reverse proxies and
  network interruption. Mobile Chromium emulation is not an iOS device test.
- Full native API route/spec coverage and streaming headers documentation.
- Broaden native component coverage beyond cache/session tests and the browser
  flows. Historical Jellyfin tests have been removed; plugins and other GPU
  encoders stay out of scope.

Vite reports a large initial bundle warning; loading succeeds, but further code
splitting is advisable before optimizing deployment performance.

## Cast and filmography validation

Validated with disposable PostgreSQL 17 and generated FFmpeg fixtures:

- `make -C backend test lint` and `make -C backend test-integration` pass,
  including migration 00006 up/down, NFO cast import, mocked TMDB credits,
  actor identity, catalog authentication/permissions, UUID validation, cursor
  pagination/filter binding, and preservation on manual metadata edits. Portrait
  tests cover TMDB downloading without credential forwarding, local NFO import,
  authenticated/permission-checked serving, missing images and unsafe paths.
  Force-scan tests verify option validation/persistence, refreshing existing
  provider IDs, locked-item preservation and one metadata job per show. Folder
  tests cover numeric seasons, Specials, conflicting filenames and stable regrouping.
- `make -C frontend check build` passes (25 unit tests). Cast component tests
  cover selection, pagination, navigation, empty filmography, and Vietnamese
  missing-cast text, protected portrait URLs and image-error fallback.
- `make -C frontend test-e2e` passes in desktop and mobile Chromium. Both exercise
  the real scanner/API, open cast filmography, navigate to a title, and check
  horizontal overflow. They also select force refresh, verify its request option,
  load portraits, and check single-line long names. Cast/detail screenshots were
  inspected.

Series detail browser coverage uses 12 local cast entries to force horizontal
scrolling, verifies that synopsis/cast span the full available width and the
Episodes section stays separated, and follows an episode back to its parent
series after reloading the episode page. Desktop and mobile Chromium also check
that the episode technical sidebar remains present and the page does not overflow.

Search coverage checks that `topLevel=true` excludes episodes in SQL before
pagination, binds cursors to the filter, validates booleans and respects library
permissions. Explicit parent episode lists remain available. Browser coverage
opens a legacy search URL with `kind=episode`, verifies movie/series-only results
and filter options, then opens a series and its episode list.

TMDB networking was mocked in integration tests; live provider responses and
physical mobile devices were not tested. Existing catalog titles need a metadata
refresh as described in README. The production build retains its chunk-size
warning.

## Adaptive buffer validation

Buffer policy unit tests cover sustained fast transfers, slowdown and recovery,
playback speed, high-bitrate memory estimates, invalid samples, session reset,
and respecting hls.js quota recovery. The browser suite generates a seven-minute
H.264 fixture, exercises the real API/transcoder remux path, and verifies over
120 seconds of contiguous forward buffer on fast localhost in desktop/mobile
Chromium. Existing seek, cleanup, playback and subtitle flows also pass.
This does not validate physical mobile memory limits, real WAN fluctuations,
native Safari HLS behavior, or NVENC production speed. Browser memory and server
segment availability may keep actual buffering below the requested target.

## General settings validation

`make -C backend test lint` and the disposable PostgreSQL integration suite pass.
Coverage includes admin-only read/write, defaults without a settings row, partial
updates preserving known/unknown values, schema validation, live server naming,
and using the configured watched threshold while rejecting stale progress.
Mocked provider tests verify metadata language, automatic-job gating, explicit
refresh while automatic lookup is disabled, and disabling new portrait downloads
without losing cached portraits. No live TMDB account was used.

Frontend checks/build and desktop/mobile Chromium browser tests pass. Component
tests cover dirty-field patches, discard, failed-save preservation and disabled
TMDB controls. Browser tests verify persistence after reload, partial request
bodies, discard, no Reduce motion control and no horizontal overflow. General
settings screenshots were inspected. Operating-system reduced-motion CSS remains
in place; no new per-app motion override is persisted.

## Audio and YouTube validation

Validated on 2026-09-21 using disposable PostgreSQL 17, generated FFmpeg media and
Mutagen 1.47.0, with no personal database involved:

- `make -C backend test lint build` passes and builds all four application binaries.
- `make -C backend test-integration` passes, including migration 00007 up/down,
  route validation and permissions, stale tag fingerprints, atomic publication
  recovery, downloader claims/retries and actual audio-only AAC HLS with video
  encoding disabled. Real tag round trips cover eight audio containers.
- `make -C frontend check build` passes with 30 tests across eight files. Coverage
  includes selected-field batch edits, permission gates, per-user queues, browser
  codec support and import preview submission. The Vite chunk-size warning remains.
- `make -C frontend test-e2e` passes all four desktop/mobile Chromium cases for
  existing video flows and audio scan, on-disk tag edits, rescans, persistent
  playback and import task UI. Mobile Chromium is browser emulation.
- The backend Docker image builds with Python, Mutagen, yt-dlp and Node included.

Deterministic downloader tests use a test-only provider adapter with real audio
fixtures to verify partial failures, completed-video deduplication, chapter
preservation and staging cleanup. Production does not use fixture responses.
A live YouTube attempt required sign-in/bot verification, reproduced directly with
yt-dlp; real public downloads remain unverified in this environment. No cookie or
authentication bypass was introduced. Live MusicBrainz/Cover Art Archive, physical
Safari/mobile background playback and NVENC hardware were not newly verified.
See audio.md for dependencies, writable audio mounts and provider limitations.

Audio library UI follow-up (2026-09-22): the creation form now includes Music,
Podcasts and Audiobooks, matching the library filter. Vietnamese component tests
select each type and submit an import-only library with empty local folders.
Import destination tests also cover missing libraries and insufficient permission.
`make -C frontend check build` passes with 35 tests; the chunk-size warning remains.

Downloader diagnostics follow-up (2026-09-22): backend tests/lint/build and the
disposable PostgreSQL integration suite pass. Diagnostic tests cover provider
categories, missing executables, cancellation, real subprocess exit codes and
non-disclosure of signed URLs/tokens. The partial-download integration fixture
verifies that HTTP 403 survives in the entry error and that logs include job,
source, video and exit-code context while completed downloads remain deduplicated.
Frontend checks/build pass with 37 tests, including Vietnamese delete confirmation,
translated entry errors and previous errors displayed during a pending retry.
No live YouTube download or deployed-server reproduction was performed for this fix.

Metadata refresh tests cover both request modes, required/invalid mode validation,
admin authorization, payload persistence, missing-episode scheduling, locked and
already identified episode preservation, and execution-time rechecks. Frontend
tests cover both choices, replacement confirmation, missing-mode submission, and
Vietnamese labels and separation from the manual Save form. Provider responses are mocked; live TMDB is not exercised.

Home resume validation (2026-10-04): backend tests/vet, disposable PostgreSQL 17
migration/server/worker integration with generated FFmpeg fixtures, all 45
frontend tests, typecheck, production build and lint of changed frontend files
passed. Regression coverage includes more than eight unfinished episodes,
recent-first ordering, timestamp ties across pages, library/user isolation,
saved playback position/duration, removal of completed media, Home resume actions,
English/Vietnamese progress labels and zero/overrun durations. Full frontend lint
is currently blocked by the pre-existing unused `serverName` argument in App.tsx.
The installed server and the user's specific media have not been verified.

Playback preference validation (2026-10-04): 60 frontend tests, typecheck, build,
changed-file lint, backend tests/vet and disposable PostgreSQL 17
migration/server/worker integration with generated FFmpeg fixtures passed.
Coverage includes Vietnamese aliases/track titles/upload names, English locale,
manual subtitle Off, disabled capabilities, bandwidth budgets/headroom,
playback speed, sustained variable samples/cooldown, bounded range probes,
ignored Range responses, cancellation, position-preserving quality changes,
manual quality overrides and automatic/manual server bitrate policy.
Contracts were regenerated before frontend API types. Real NVIDIA encoding and
adaptation on physical devices/remote networks were not verified in this run.
The existing full-frontend lint failure in App.tsx remains outside this change.

## Source-aware playback refactor

Decision tests cover MP4 HEVC/AAC direct play; MKV HEVC/AAC video/audio copy;
MKV HEVC/TrueHD and DTS video copy with audio conversion; unsupported HEVC and
Main10 conversion; supported Main10 preservation; PGS burn-in; source profile,
level/dimension mismatch; and prepared SRT external rendering. Probe tests cover
Main10 depth, HDR, rational frame rate, audio channels/sample rate and PGS type.

A generated libx265 HEVC/AAC MKV fixture runs through real FFmpeg stream copy
into fMP4 and is reprobed to confirm HEVC/AAC preservation. Partial output does
not become ready. API range tests verify 206/416, Content-Range/Content-Length,
Accept-Ranges and immediate visibility after output growth. PostgreSQL API
integration verifies persisted decisions, no direct-play worker job, remux at a
requested timestamp, fMP4 ranges, token enforcement, stop revocation and scanner
text subtitle preparation. Existing legacy HLS, auth, worker leases/recovery and
OpenAPI/request validation tests remain in the suite. Migration 8 uses the
external CLI and disposable PostgreSQL for up/down/dirty-schema checks.

Frontend tests cover Main10 capability strings, MKV-independent video support,
AAC remux selection, HDR decodingInfo checks and rejection without measured HDR
support. The MSE reader test verifies bounded byte offsets, incremental appends,
worker EOF and URL release on cancellation. Native browser/device HEVC and HDR
support varies; generated FFmpeg fixtures do not establish support on real client
hardware. NVENC execution requires an NVIDIA test host. Remux copy seeks depend
on source keyframe placement.

Validation on 2026-10-05: backend unit tests, go vet and disposable PostgreSQL
integration passed. Frontend type checking and 65 unit tests passed; changed
frontend files pass ESLint. Focused source-aware playback E2E passed on desktop
and mobile Chromium (`make -C frontend test-e2e E2E_ARGS=playback.spec.ts`). The
full E2E run passed its four audio/fMP4 checks but two broader app tests stopped
at the existing fullscreen Speed-menu assertion: the current stylesheet hides
fullscreen controls. Full frontend lint also reports the pre-existing unused
`serverName` argument in App.tsx. Neither unrelated UI behavior was changed.
Worker tests include active fMP4 stop/cancellation and removal while bitrate
measurement is blocked. MSE quota eviction retries preserve the video stream;
network/read failures do not request video conversion. The ignore rule for media
storage is anchored to backend/media so internal/media source is visible to Git.


## HDR tone mapping

HDR10/PQ and HLG fixture tests run the actual zscale/tonemap filter chain on
generated 10-bit HEVC patches. They verify changed, monotonic, legal-range SDR
luminance, neutral chroma, BT.709/TV/8-bit output and removal of HDR frame
metadata. Fixture encoding uses software encoders only in tests; production
video encoding remains NVENC-only. HDR10+ uses the static base; Dolby Vision
conversion is explicitly rejected. Decision tests preserve supported HDR
direct/remux paths and cover modern and legacy conversion requests.

Disposable PostgreSQL integration checks persistence and queued decisions, plus
worker FFmpeg arguments and toneMapped stream reports for HDR alone, ASS and
bitmap burn-in. Subtitle composition follows tone mapping. These worker tests
capture encoder arguments; they do not execute NVENC or validate real PGS
rendering. Frontend tests cover the conversion indicator in English/Vietnamese
and its absence for copied HDR.

Validation on 2026-10-05: disposable PostgreSQL integration passed. Real filter
tests use Debian bookworm FFmpeg with libzimg (the production image's distro);
the local Homebrew FFmpeg lacks zscale. Actual NVIDIA encoding, HDR display
playback and subjective tone-map appearance still require a real-device check.
