# Native architecture and implementation status

The root contains orchestration and documentation. frontend/ is the React app;
backend/ contains exactly api, scanner, transcoder and downloader commands. PLAN.md
remains the accepted scope; this document tracks implementation.

## Data and control flow

1. Fiber routes register through typed route.Get/Post/Put/Patch/Delete wrappers.
   These call the shared Register[B,Q,P,R] implementation, which binds inputs,
   attaches auth and directly registers a Fiber handler. DTO jsonschema tags
   generate schemas with invopop/jsonschema; santhosh-tekuri/jsonschema validates
   those same schemas at runtime. The wrapper owns OpenAPI 3.1 operations and
   streaming descriptors without Huma. api openapi exports the
   contract without PostgreSQL; frontend/src/native/generated.ts is generated
   by openapi-typescript and consumed by openapi-fetch.
2. api creates catalog, account and playback records through sqlc queries/pgx.
   Login tokens are opaque with server-side hashes. Library access is checked
   for catalog, artwork and playback. Playback URLs use a session token.
3. scanner claims scan/metadata jobs from PostgreSQL, resolves paths within the
   configured media root, probes files and updates catalog records. Rescans
   reuse unchanged video probe data but rediscover subtitles/local metadata.
   Prepared embedded text subtitles reuse valid cache when the video fingerprint
   is unchanged; missing/corrupt cache and changed sources are extracted again.
   External sidecars are refreshed independently of the video fingerprint.
   Only a successful complete scan marks missing files unavailable. Discovery
   deduplicates resolved paths before probing. Jobs report processed/total video
   files through lease-fenced updates; progress stays below 100 until completion.
   Library counts include available indexed media files, not series parent items.
   Library views poll every three seconds and display the most recent scan.
4. Playback uses a source-aware decision layer: direct play, then video-copy
   remux/direct stream, then NVIDIA video encoding. Scanner persists video
   profile, level, pixel format/bit depth, dimensions, bitrate, frame rate and HDR
   transfer, primaries, matrix, range and side data, plus audio channels/sample rate/bitrate and subtitle type.
   Probe version 3 invalidates old cached probes on the next library scan.
   The browser reports tested source configurations through PlaybackRequest's
   optional capabilities object. Codec strings distinguish HEVC Main/Main10;
   canPlayType, MediaSource.isTypeSupported and MediaCapabilities.decodingInfo
   check native and MSE playback without user-agent rules. Native and MSE audio
   capabilities are separate. HDR requires an explicit decoding capability;
   Dolby Vision/HDR10+ playback is conservatively unavailable in this detector.
   Unknown profiles are not advertised. Requests without capabilities retain
   the legacy method selection and HLS remux transport.
   PlaybackDTO keeps method/URL compatibility and adds decision (mode, actions,
   container, reason) and protocol. Decisions are persisted with the session.
   Direct play serves the original with HTTP ranges and queues no worker job.
   New video remux sessions use FFmpeg video copy into growing fragmented MP4,
   with supported audio copied and unsupported audio converted to stereo AAC.
   HEVC is tagged hvc1 for MP4 browser compatibility. The API streams fresh size
   snapshots with bounded ranges, avoiding cached growing-file sizes. The MSE
   reader requests 256 KiB chunks, limits buffered media to approximately 30
   seconds ahead, removes media older than 60 seconds and releases its object URL
   on stop. Readiness requires a complete moof/mdat fragment; EOF requires a worker
   completion marker. A copied source GOP can delay the first fragment.
   The transcoder owns all playback FFmpeg processes; none run in HTTP requests.
   Unsupported video, requested lower resolution/bitrate, subtitle burn-in or
   explicit decode fallback require NVENC. Capability-aware conversion outputs
   H.264/AAC HLS even when the legacy output setting is HEVC. Video encoding has
   no CPU fallback. Required HDR10/PQ and HLG transcodes use a named CUDA device
   for NVDEC, `tonemap_cuda`, `scale_cuda` and NVENC, converting to limited-range
   8-bit BT.709 SDR. Metadata filters leave the pixels on GPU. No CPU tone-map
   fallback is available. With text/ASS/bitmap burn-in, SDR frames are downloaded for
   CPU subtitle composition and uploaded to the same device; bitmap resize
   follows overlay to retain original subtitle coordinates.
   HDR10+ uses its static HDR10 base; dynamic metadata is not applied. Dolby
   Vision profile 8 with an explicitly signaled HDR10-compatible base (compatibility
   ID 1, base present, enhancement layer absent, PQ transfer) uses that base only.
   Dolby Vision RPU/frame metadata is removed; dynamic Dolby Vision processing
   is not performed. Other Dolby Vision streams remain rejected. Probe version 4
   retains the compatibility fields; a library rescan refreshes older cached probes.
   Direct/remux HDR remains untouched when supported by the client. The worker
   independently checks source HDR metadata and required filters, including
   `tonemap_cuda` and `scale_cuda`, before encoding. The image pins a checksum-verified
   standalone FFmpeg build with these CUDA filters; old Debian FFmpeg is unsupported
   for GPU conversion. SDR transcodes also use CUDA decode/resize rather than the
   software HEVC decoder/scale filter. Hardware/driver failure fails the session and the player
   shows an HDR-specific NVIDIA error. Tone mapping precedes subtitle
   composition; SDR output strips mastering/content-light/HDR10+ frame metadata
   and sets explicit BT.709 color tags. Decisions and stream reports include
   toneMapped, shown in the player. Rescan libraries to refresh cached colors.
   Quality limits are ceilings and do not encode smaller/lower-bitrate sources.
   Progress sequence numbers prevent stale updates from winning.
5. Downloader owns public YouTube previews, downloads and six-hour append-only
   playlist subscriptions. Scanner also owns audio discovery and journaled tag
   writes. See audio.md for permissions, publication recovery and dependencies.
6. Workers use PostgreSQL leases, heartbeats and bounded attempts. The processes
   share media and writable cache paths on the same host. Video media stays
   read-only for API/transcoder; scanner needs writable audio mounts and optionally
   writable video directories for explicit subtitle edits, and scanner/downloader share
   a writable import volume. API/transcoder read media and imports only. Encoding
   concurrency is coordinated through a PostgreSQL advisory transaction lock.
   Playback stop/expiry and job-context cancellation finish as cancelled with an
   empty error field and an info-level "job cancelled" event. Genuine FFmpeg,
   database and job execution failures remain error-level events.

## Implemented slices

- Monorepo layout, four binaries, migrations, queries, DTO registration,
  generated client, container images and CI checks.
- Music, podcasts and audiobooks, optional MusicBrainz suggestions, file-backed
  single/batch tag editing, YouTube imports and a persistent audio player with
  queues, chapters, speed and sleep timer. See audio.md for operational details.
- Setup/login/logout, user administration and library permissions.
- Library paths, manual/scheduled scan, jobs/workers, movie/episode catalog,
  local NFO/posters, optional TMDB lookup and metadata editor.
- Direct MP4/WebM, fMP4 remux and NVIDIA NVENC HLS, track selection, watch state, resume, next episode,
  persistent player and fullscreen.
- Seek reuses the active session for buffered media or published HLS fragments,
  including unbuffered segments still available in the EVENT playlist. Native HLS
  uses the media seekable ranges. Targets before the session start or beyond
  available segments create a new session. Local seeks preserve paused state and
  translate the movie position using the session offset.
- Player seeking retains the last decoded frame while a new HLS session prepares,
  with a compact loading indicator. UTF-8 SRT/VTT uploads are stored as plain-text
  cues in PostgreSQL per account/file, protected by library access and ownership.
  Uploaded subtitles render through FFmpeg/libass with the same style as embedded
  text burn-in. All subtitle timing changes restart NVENC after a short debounce.
  Positive delay shows subtitles later. ASS styling is not preserved by uploads;
  captions are part of the video and accompany native fullscreen, PiP and cast
  when the receiver supports the playback stream. See seek-previews.md for thumbnail hover research.
- The seek track draws actual HTML media buffered ranges, including gaps and
  HLS session offsets. Loading shows measured segment transfer speed (bytes/sec)
  from hls.js loader stats. Native playback uses completed Resource Timing entries
  when available; missing/cache-only measurements remain explicitly unavailable.
  Recent measurements expire after three seconds, and all metrics reset on a
  new playback session. Buffer length is never used to guess download speed.
- Playback quality, audio, subtitles and speed selectors stay on the same row as
  Play/Volume as compact text buttons with the current value. Clicking a
  button opens a checked-option menu with Mantine ScrollArea; buttons stay on
  one horizontally scrollable row on narrow screens. Menus portal into the player
  root to avoid clipping while remaining visible in fullscreen. Seek and
  volume tracks share a slim 3px appearance. Subtitle upload lives in the subtitle
  menu, adds the saved track and selects it. A timing button in the existing control
  row opens a slider popover for personal delay/file-default reset and burn-in notes; enabling subtitles or
  opening timing never adds a row or changes the video height. Popovers remain
  inside the fullscreen player and timing supports Escape/focus trapping.
- A playback indicator uses the worker output report to confirm video encoding,
  independently of the requested session method or server GPU configuration.
  Remux/video-copy is never labeled NVENC; legacy HLS sessions without a report
  show HLS with encoding details unavailable. The report describes how the stream
  was produced, not whether the GPU is currently busy. It resets on session changes.
- The indicator shows decoded video dimensions and total Mbps. Its popover adds
  separate video/audio bitrates and codecs. Direct streams use stored ffprobe
  metadata plus file size/duration for the total. HLS workers measure packet bytes
  and timestamps from the first completed segment. Workers publish a pending
  report and mark playback ready immediately, then measure in the background.
  Completed reports replace stream-info.json atomically. Measurements have a
  15-second timeout and are joined/cancelled with the worker lifecycle; failures
  leave playback usable. Frontend refreshes pending metrics separately, at most
  20 times, cancelling on session change. API never runs FFmpeg.
  HLS numbers are explicitly a segment sample, not target bitrate or download
  speed. Missing rates stay unavailable. Scanner probe version 1 retains bitrate;
  the next library scan refreshes older cached probes once. The quality selector
  remains the requested limit, not the measured resolution.
- Discover uses a selectable spotlight, direct playback, resume cards, separate
  movie/series rails with Mantine ScrollArea, library shortcuts and a favorites
  entry point. All content comes from the native catalog; posters and metadata
  are optional, and empty libraries have an onboarding state.
- Library archive cards with poster previews, file counts and scan status;
  series libraries list shows only, with episodes inside the show detail page.
  Scanner groups shows by the first directory under each library root. Season
  directories support S01, Season 01, Session 01, 01 and Specials. Folder names supply show
  titles; filenames supply episode numbers (S01E01, E01, ep1 or 01).
  Files directly in a library root retain filename-based grouping. A rescan
  reparents existing episodes without changing their IDs or watch progress,
  and removes empty series left by the previous naming-based grouping.
  Library catalog queries without an explicit kind/parent default to root items
  before cursor pagination. Global search requests `topLevel=true`, filtering to
  movies/series in SQL before pagination; its kind selector excludes episodes
  and ignores legacy `kind=episode` URLs. Explicit episode queries remain available
  for series detail and other catalog views.
  catalog search/filter controls and an optional Explorer-style grouped view.
  Title grouping normalizes accents, sequel suffixes and shared prefixes on the
  loaded catalog pages. Groups expand as infinite scrolling loads more titles;
  counters describe loaded items. Grouping is a view preference in the URL and
  never merges files, metadata or watch state.
- Library administration includes totals, name/type filters, scan status,
  storage paths, last successful scan and scheduling details. The library editor
  separates identity, folders and scheduling in a Mantine ScrollArea modal.
- Native admin encoding settings and English/Vietnamese UI.
- Task history joins jobs to their movie/series/library and shows worker role,
  attempts, creation/activity timestamps, scan counts, cancellation requests and
  job IDs for correlating worker logs. The page includes library schedules and
  filters the latest 100 jobs; it is not a complete audit history.
- User administration has searchable role-filtered account cards with explicit
  library access. Metadata editing separates local fields from TMDB matching.
  Matching strips release filenames and ranks normalized/localized/original
  titles with year agreement. A sole TMDB result is auto-applied when its score
  is strictly greater than 50, including when the year is missing or different.
  With multiple results, only a unique exact normalized title with a
  matching known year is auto-applied (or a unique exact title when year is
  unknown). Multiple fuzzy or tied candidates require manual identification and are not
  silently applied. Existing provider IDs and metadata locks are respected.
  Ranking scores are heuristic, not probabilities. No LLM integration is used.
- Item details show synopsis, year/episode, watch progress, file versions and
  per-file size, resolution, duration, video/audio/subtitle tracks. Size and
  dimensions come from stored scan data; missing metadata is shown explicitly.
  Selecting a version updates technical details and the playback source together.

main.tsx mounts native/App.tsx. Historical Jellyfin modules, their dependencies
and obsolete fixture tests have been removed.
The native implementation intentionally exposes a smaller feature set than the
old Jellyfin administration UI. See validation.md for outstanding acceptance.

## Operations

Backend configuration uses Viper to read .env from the working directory, with
process environment values taking precedence over the file and defaults.
Missing .env is allowed; malformed files fail startup without exposing their
contents. Each load uses an independent Viper instance.

The official migrate CLI applies SQL migrations independently of the application.
Numbered .up.sql/.down.sql pairs live in backend/db/migrations;
make -C backend migrate invokes the locally installed CLI. api openapi emits JSON to stdout. See migrations.md for existing Goose databases and recovery procedures.
Start migration before the four long-lived processes. DATABASE_URL,
JFE_MEDIA_ROOT, JFE_CACHE_ROOT and JFE_IMPORT_ROOT must agree across processes.
An empty import root disables YouTube imports. Back up PostgreSQL;
retain original media independently. Cached artwork/HLS is generated data.
Never remove a library by deleting source media. Log output is structured through
zerolog; HTTP logs omit query strings and authentication tokens.
Server errors are logged at error level with the original cause, requestId,
method, route template and final HTTP status. The shared error handler covers
typed routes, streaming failures and recovered panics; internal causes remain
private in HTTP responses. System errors identify whether counting users,
reading encoding settings or parsing those settings failed.

Video encoding modes are disabled (default) and nvidia. API negotiation rejects
encoding while disabled, and the worker rechecks settings before starting FFmpeg.
Only h264_nvenc is used for video encoding; GPU failures never use CPU fallback.
All video transcodes use CUDA decode/resize and NVENC; subtitle composition and AAC audio can use CPU. See README for NVIDIA deployment.

## General administration settings

The General settings page follows Silo's approach of grouping essential controls
by purpose, with explicit Save/Discard and provider configuration status. Its
implementation is native JFE; it does not import Silo settings code or out-of-scope
providers. Library, user, NVENC and worker/job administration retain their pages.

`GET/PATCH /api/v1/admin/settings` require admin access and use explicit DTOs.
Only edited fields are sent; SQL atomically merges the JSON patch into the
`general` settings row, preserving unrelated and unknown keys. Missing fields
resolve through shared API/scanner defaults without requiring a new migration.
The public system response and login/sidebar use the configured server name.
TMDB status means token configured, not tested connectivity; secret values are
never sent to the frontend. Metadata controls are disabled without an API token;
operators must configure matching credentials for scanner too.

The scanner reads preferences for new automatic jobs; jobs carry an `automatic`
flag and recheck the toggle before provider lookup. Force refresh and manual
identification bypass the automatic-lookup toggle, preserving existing lock
semantics. Both metadata search and detail requests use the configured language.
Portrait downloading can be disabled without removing previously cached images or
local NFO portraits. The watched threshold is read on progress reports, without
retrospectively rewriting watch state. Changes need no process restart.
The retired personal reduced-motion store/control is removed. CSS and native
scrolling continue to respect `prefers-reduced-motion` from the OS/browser.

## Adaptive playback buffering

Each hls.js session starts with a 60-second forward target and 30-second back
buffer. Completed main media fragments feed a rolling three-sample policy using
media duration / request duration, adjusted for playback speed. All three samples
must sustain at least 2x playback speed for a 180-second target or 5x for 300
seconds. A slow sample immediately reduces future loading; already buffered media
is not flushed. Invalid/aborted/init-segment samples are excluded.

The largest recent bytes-per-media-second estimate limits the total time target
to approximately 128 MiB including the back buffer, with a six-second forward
floor. This is an estimate, not a hard browser-memory cap: muxing, decoding,
variable bitrate, and whole-fragment overshoot add overhead. `maxBufferSize=0`
disables hls.js's independent byte-derived time floor; both time limits receive
the adaptive target. Quota errors or hls.js lowering its own limit suspend growth
for that session so recovery is not undone. The policy resets on session changes.
Native media buffering remains browser-controlled with `preload=auto`. Available
server segments and transcoder speed still bound actual HLS prefetch.

## Cast and actor filmography

Detail content uses one column when no active media file exists (including series),
and retains the technical-info sidebar for playable movies/episodes. Series
episodes live in a separately titled section with spacing and a divider after
the cast scroll area. Episode breadcrumbs link to `item.parentId`, so returning
to the series also works after opening a direct episode URL or reloading.

Scanner stores ordered cast credits on `items.cast_members` (migration 00006).
TMDB detail requests append `credits`; NFO actors support `name`, `role`, and
optional `tmdbid`. TMDB person IDs produce stable UUIDs independent of the
display name. Without a provider ID, local actors use normalized names: namesakes
can merge, and local-only identities do not automatically match TMDB identities.
Scanner downloads TMDB `profile_path` portraits from the fixed image host and
imports local NFO actor `thumb` paths confined to the configured media root.
Images are size/dimension bounded, decoded, normalized to JPEG and atomically
cached in artwork. `GET /items/:id/cast/:personId/image` authenticates the token,
checks access to that item and verifies cast membership before serving the file.
DTOs expose only these protected image URLs. Missing portraits use initials;
names are rendered in a smaller single line with ellipsis and full-name tooltip.

`DetailDTO.cast` exposes explicit credit DTOs. `GET /api/v1/items?personId=<uuid>`
uses the existing permission-filtered, cursor-paginated catalog and a GIN index
on credits. Cursor fingerprints include the actor filter and user. No external
filmography or inaccessible library titles are returned. The frontend opens a
Mantine ScrollArea modal with title cards and incremental pagination; selecting
a title navigates to its detail page without disturbing the persistent player.
Manual metadata edits preserve cast. `POST /libraries/:id/scan?forceMetadata=true`
persists the option in the scan job payload. It imports local metadata and queues
TMDB metadata jobs even for existing provider IDs, while respecting metadata
locks. Without a TMDB token only local metadata is imported. Scheduled scans keep
the normal behavior. Metadata jobs run independently after discovery, so scan
completion does not mean every TMDB refresh has finished. Series metadata is
processed once per show per scan, never once per episode; episode NFO is still
read separately. Pending/running jobs retain the existing deduplication behavior.

For structured shows the first folder below the library root defines the show
identity, with the second folder defining its season. `Season 02`, `S02`, numeric
folders, and `Specials` are recognized and take precedence over filename season
numbers. Unknown season-folder names still group under the show, using available
filename numbering. Flat filename grouping remains for existing flat libraries.
Rescans preserve file/item IDs and watch state while correcting parent/season
links and removing empty legacy series records.

Show metadata refresh accepts a required `mode` (`replace` or `missing`) at
`POST /items/:id/metadata/refresh`. Missing mode requires an identified series,
skips the series lookup/update even if the series is locked, and queues only
unlocked episodes without a provider ID. Episode jobs retain the mode and recheck
provider IDs when executed so retries do not refresh already populated episodes.
Replace mode keeps the existing series/episode refresh and lock behavior.

The admin show-detail action opens a dedicated refresh dialog with the two modes.
The manual metadata editor contains no refresh controls; its Save action only
saves manual fields. The refresh dialog submits the selected mode directly, with
an overwrite warning and explicit replacement action for replace mode.

The resume catalog orders unfinished user-state records by updated time descending,
with title/ID ties and a timestamp-bearing pagination cursor. Library permissions
and user isolation apply before pagination; resume requests scoped to a library
include episode children. Item DTOs expose duration from the first available file
in path order, matching the Home resume action's file selection. Unknown duration
is zero. Home renders clamped progress and invalidates its resume query after a
successful video progress save.

Playback preferences use the interface language until the viewer chooses a
subtitle manually, including Off. Language matching normalizes accents/case and
token boundaries for English/Vietnamese codes and labels. Matching uploaded text
subtitles take priority over matching embedded/sidecar tracks; all subtitles are gated
by the transcoding capability. Playback waits for the initial capability and
subtitle queries so it does not first launch an unwanted session.

Automatic video quality starts with the browser's downlink hint when available.
Native direct playback measures two bounded 256 KiB ranges through its existing
short-lived authorized stream URL, with a three-second timeout per sample and
cancellation on session cleanup. Responses ignoring Range are cancelled. HLS
uses completed, non-aborted media transfer samples; cached/unknown speeds stay
unknown. Fetch probe timings are excluded from native media observer samples.
The selector keeps 35% network headroom, reserves audio bandwidth, accounts for
playback speed and avoids increasing source dimensions. Two lower or four higher
samples and a 30-second switching cooldown prevent repeated restarts.
Changes retain position, cancel prior requests and clean up the prior session.

PlaybackRequest.autoQuality allows a lower measured MaxBitrate under the selected
server resolution profile's bitrate ceiling. Manual profiles retain server
bitrates. Original source playback remains preferred when it fits the connection;
automatic burn-in also caps bitrate using the source estimate. The worker still
owns all FFmpeg processes and video encoding remains NVIDIA-only. With encoding
disabled, quality stays at Original and its selector is disabled.

## Playback transport and lifecycle details

POST /api/v1/playback remains the negotiation/start operation. Direct sessions
return protocol=file and /stream/original; remux sessions return protocol=mp4 and
/stream/stream.mp4; video transcode sessions return protocol=hls and
/stream/index.m3u8. The explicit decision modes are direct_play, remux and
transcode; the legacy method name direct remains unchanged. All streams enforce
session credentials, enabled accounts and current library access.

Playback start creates at most one worker job for the session. Existing leased
job claims and cancellation own the process lifetime. Stop cancels the job and
CommandContext process; browser cleanup aborts reads and deletes the session.
Individual range-request completion does not kill a shared worker process.
Abruptly disconnected clients expire after the existing five-minute progress
idle timeout; credentials also expire after six hours. Worker cleanup removes
stopped/failed/expired playback files. Remux writes output to disk without loading
the source into memory; cache disk usage can grow to the size of a session output.

Direct seeking uses HTTP ranges. Remux seeking inside MSE buffered/seekable ranges
uses currentTime; seeking outside available ranges creates a new session with
FFmpeg input -ss at the requested movie timestamp. Stream-copy seeks retain source
keyframe granularity. Transcode seeks retain the existing HLS session behavior.

Scanner prepares SRT/WebVTT embedded/sidecar tracks as bounded plain-text cue
files in shared cache/subtitles, atomically published and listed through the
existing subtitle API. Prepared embedded text cues are reused for FFmpeg rendering
without rereading the entire movie. Sidecars render from their original files to
preserve formatting; unavailable embedded caches fall back to the original track. Uploaded SRT/WebVTT
remain stored per account in PostgreSQL; the transcoder verifies file and session
ownership, writes a session-local SRT, converts it to ASS, and uses the same libass
filter/style and delay as embedded text burn-in. No FFmpeg runs in HTTP requests.
All subtitle playback requires NVENC; there is no browser overlay in the player.
Text subtitles over 512 KiB are not prepared. The editor retains a local HTML
preview for immediate timing feedback, which is approximate rather than libass.



Original subtitle management is documented in [subtitle-editor.md](subtitle-editor.md).
The editor's source-management endpoints use typed routes and scanner jobs; upload
deletion is owner-scoped. `JFE_SUBTITLE_EDITING` enables a deliberate scanner-only
exception to read-only video mounts. Preview sessions are marked in PostgreSQL so
progress calls only renew playback and cannot change user watch state.


Personal subtitle timing uses typed GET/PUT `/files/:id/subtitle-timing` routes
and PostgreSQL `personal_subtitle_timing` (migration 00010). Access follows viewing
permissions, independent of original editing. Keys isolate users/files/sources;
original track and prepared-ID aliases share a source offset. Probed track-list revisions
prevent stale selection writes and invalidate offsets after track remapping.
Original subtitle publication atomically clears original-source offsets with the
catalog refresh; personal uploads retain their offsets. The frontend applies
saved milliseconds to the NVENC subtitle delay parameter. Slider
changes are local until release; queued saves are serialized and retain the
initiating account's credential. Errors are shown rather than reporting success.

## Playback generation pacing

Video transcodes read at up to 2x media time, with a 16-second startup burst and
2.25x temporary catch-up. This matches the native player's maximum 2x speed and
avoids unbounded full-speed background encoding. Input pacing flags precede the
input file; direct play, video-copy remux and audio-only conversion are unchanged.
FFmpeg writes one-second machine-readable progress (fps/speed) in the playback
cache, removed with the session. The limit does not bound the buffer during pause;
CPU demux/audio work and NVIDIA session overhead remain. Real-time capability and
speed at 2x still depend on hardware and storage.
