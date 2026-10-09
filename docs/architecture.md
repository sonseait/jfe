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
   Video catalog SQL excludes movies/episodes without an available file and
   series without available children, before filtering/pagination. Episode views
   use the same availability rule. Moving a file creates a new path-based identity;
   the old unavailable item retains metadata/user state but is hidden after scan
   reconciliation. A completed scan timestamp is included in the frontend library
   catalog key so old pages are not reused. No original media or history is deleted;
   history is not automatically merged into the new item.
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
   8-bit BT.709 SDR. For HDR with a resolution ceiling, `scale_cuda` first
   resizes at the source bit depth, then `tonemap_cuda` converts to NV12/SDR.
   Text subtitles render after the resized tone-mapped frames. Bitmap subtitle
   burn-in retains tone-map/composition at source resolution followed by resize
   to preserve its canvas coordinates. Without a ceiling, tone mapping uses the
   source resolution. Metadata filters leave the pixels on GPU. No CPU tone-map
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
  titles with year agreement. If a non-English search has candidates but no
  recommended match, a second English search supplies matching aliases joined
  by TMDB ID. Candidates, display titles and overviews remain from the configured
  language; English aliases only affect ranking. API manual identification and
  scanner automatic lookup share this logic and unchanged acceptance thresholds.
  A sole TMDB result is auto-applied when its score
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
semantics. Metadata search and detail requests use the configured language;
inconclusive localized searches additionally compare English names for the same
provider IDs without changing the metadata language.
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
The scanner registers JPEG/PNG/GIF/WebP decoders and decodes by image content,
independent of the source suffix. Remote posters use the same bounded decode/JPEG
publication path as portraits. Poster fetching precedes portrait fetching, sharing
a 45-second artwork deadline (20 seconds per HTTP request). Optional artwork
failures log warnings with job/item/actor identifiers and preserve old image
references; they do not abort saving a successful metadata identification. Unsupported
or malformed image data is rejected before publication, with its detected MIME
type in the diagnostic and no response body logging. Job cancellation still aborts
publication. Metadata detail/provider and database failures remain job failures.

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

Player pause intent persists across session changes: loaded metadata only starts
playback when the viewer still wants it. Closing the player increments its generation, invalidating pending
next-episode responses. Cleanup cancels queued progress requests, saves a final
position with a two-second deadline, then deletes the session without waiting for
an unbounded request queue. One already-submitted session creation may finish
after close; its response triggers deletion rather than playback.

Automatic video quality is chosen once per opened video from the browser's
network downlink estimate. The chosen bitrate and resolution are retained across
pause/resume, seeks and subtitle/audio changes; returning from a manual quality
to Auto reuses the initial choice. Missing network estimates start at Original.
The selector keeps 35% network headroom, reserves audio bandwidth and avoids
increasing source dimensions. Transfer samples only update download metrics and
HLS buffer targets; they never change quality or create a new playback session.
There are no extra range probes delaying startup. Opening another video picks
Auto again. Decode fallback and explicit viewer changes can still create sessions.

PlaybackRequest.autoQuality allows a lower initial MaxBitrate under the selected
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
ownership, writes a session-local ASS directly from plain-text cues, and uses the
same libass filter/style and delay as embedded text burn-in. Import parses HTML
entities once; direct ASS serialization preserves decoded Unicode without
re-escaping HTML. Braces are escaped and literal backslashes are separated from
ASS control characters by an invisible word joiner. Original sidecars and
uncached embedded tracks still use FFmpeg decoding to preserve source formatting;
that path does not apply the application's HTML entity normalization. No FFmpeg runs in HTTP requests.
All subtitle playback requires NVENC; there is no browser overlay in the player.
Text subtitles over 512 KiB are not prepared. The editor retains a local HTML
preview for immediate timing feedback, which is approximate rather than libass.



Scanner subtitle caches retain stable public IDs and use adjacent atomic
`.fingerprint.json` records (cache version 1). Embedded cache keys hash the resolved
source path, size, nanosecond mtime, stream selection, codec and probe/cache version;
they do not hash video contents. Existing valid embedded caches are adopted when
the database's size/mtime and probe version confirm an unchanged source. Sidecar
SRT/VTT is read directly with a 512 KiB limit and content-hashed with SHA-256,
catching same-size/mtime edits. Unchanged valid cues bypass extraction/parsing.
Failed embedded extraction or parsing is backed off for ten minutes per fingerprint;
a source/version change bypasses this backoff. Cancellation is never cached as a
failure. Missing/corrupt successful caches regenerate, and failed preparation
leaves original-track burn-in available. Source size/mtime is checked again before
publication. Scan only prepares external SRT/VTT inline and reuses validated
embedded caches; missing embedded text tracks enqueue one `subtitle_prepare` job
per media file. That job shares the source-file lock, rechecks the current database
probe and source fingerprint, extracts all missing supported tracks in one FFmpeg
demux pass (90-second bound), then updates the probe without changing availability.
No video/audio encoding occurs. Scan progress writes are throttled to 250 ms while
retaining initial/final updates. Claim ordering places subtitle preparation after
other scanner work, including metadata and user-requested subtitle downloads;
scan completion and subtitle readiness are separate. Local metadata/artwork,
ffprobe for new/changed media and directory enumeration remain scan costs.

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

## Catalog sorting

Typed catalog queries accept `sort=watching|title|newest`; existing callers that
omit it retain alphabetical order. Video catalog UI defaults to Watching first;
audio library UI retains Title A–Z. Watching means the requesting account has
position > 0 and watched=false; these records sort by user-state updated_at
descending, followed by remaining titles alphabetically. Recently added sorts by
item created_at descending. SQL sorts before LIMIT, with title/ID tie breakers.
Cursor fingerprints bind sort and filters to the requesting account; watching
rank and sort timestamp preserve the page boundary. Resume keeps its existing
updated-time order, and episode season views retain episode order. Title grouping
keeps server order for Watching first/Recently added. Sort selection is in the URL
and query key, resetting pagination on change.


## Google Cast sender and receiver playback

`CastPlayer` loads Google's CAF sender SDK once on supported secure Chromium
origins and uses the Default Media Receiver. The persistent local player releases
its playback session while Cast owns the item. Receiver selection stays in memory
for the initiating account across episodes/queue entries; it is not persisted and
is cleared on receiver disconnect. Local speed/sleep and track/quality controls
are suspended while Cast owns playback. Cast's remote controller handles transport
and volume, while ordered progress requests use the initiating account credential.
Natural completion follows the existing next-episode/audio queue behavior.
Disconnect restores the latest source position to local playback. Cleanup removes
CAF listeners and timers, stops owned remote media and releases backend sessions,
including create requests that finish after cancellation. Ownership is checked by
media content URL before stopping a receiver, so an obsolete load does not stop
new media. Page hide saves progress; it cannot guarantee receiver shutdown after
browser termination. Stream expiry remains the upper bound on unattended access.

The typed playback request accepts optional `target=chromecast`. The sender reports
a conservative receiver capability baseline independently of browser capabilities:
SDR H.264 through level 4.1, 1080p/30 fps, AAC. Backend decisions retain direct MP4
where supported, choose MPEG-TS HLS for Cast remux instead of the native player's
growing fragmented MP4, and convert incompatible/multichannel audio to stereo AAC.
Audio-only legacy playback also uses AAC for this target. NVENC decisions, burn-in,
HDR tone mapping, authorization, roots, leases and token hashing remain shared;
there is no new binary or software video encoder. HLS seeking creates a new session
at the requested source position; receiver time is offset back to source time for
progress. Stream-only CORS allows GET/HEAD/OPTIONS and Range with no cookie
credentials; every media request still verifies the playback token, expiry, user
status and library permission. The receiver receives no login credential/artwork.
URLs use the sender origin, which must be reachable by the receiver with trusted
TLS and no separate proxy authentication challenge.


## Cast reconnection and media retry

An active Cast player now treats a missing session or disconnected controller as
transport loss instead of releasing playback to the local player. CAF session
state events, controller changes, a one-second health check and browser online
notifications drive recovery. `chrome.cast.requestSessionById` rejoins the existing
session without opening a picker. The backend session stays alive through transport
recovery; returning media with the same content URL restores control and reads its
confirmed receiver time instead of reloading/stopping the TV. Missing media has a
five-second grace period before retry; receiver errors and buffering stalled for
30 seconds schedule a fresh backend playback session at the last confirmed source
position with the previous pause intent. Loads remain serialized and obsolete
resources still use owned-media cleanup.

Recovery has a shared per-item six-attempt budget, with 1/2/4/8/16/30-second delays.
Healthy playback/pause for 30 seconds resets it. Offline transport waits without
consuming attempts. Permanent playback API failures (non-retryable 4xx) and worker
failure do not automatically recreate sessions. Explicit sender stop, CAF ending,
receiver media cancellation, completion or another receiver/media taking over do
not trigger recovery. Exhausted transport recovery leaves the stream/last position
intact and offers explicit device selection. The browser can rejoin a surviving
Cast session; it cannot silently launch a destroyed session on a receiver, so only
a user click calls `requestSession`. Timers, online and CAF listeners are removed
on effect cleanup. Recovery requires a running sender page and remains subject to
browser suspension, Cast discovery, network reachability and stream expiry.


Cast receiver selection and recovery require the configured Default Media Receiver
application ID. Initial loads reject another receiver application; takeover during
playback releases JFE ownership instead of loading media into that application.
The receiver consumes server URLs through `loadMedia`; the app does not capture
browser/tab/screen frames and has no mirroring fallback. Receiver-side video
presentation is independent of the sender Fullscreen API. Chrome's Optimize
fullscreen videos control is a browser mirroring preference unavailable to the
Web Sender SDK; the application cannot force that browser setting.


## Cast loading feedback and toolbar

The Cast action is portaled into the persistent video's top toolbar; the Cast
component remains mounted across local-to-remote transitions, so moving the action
does not restart its session. Native button styling matches the existing thin-icon
controls, with explicit icon/label spacing and accessible icon-only narrow layouts.
Inactive remote panels reserve no video-player space. During fullscreen Cast the
top toolbar remains available for disconnect/close.

The Web Sender load timeout is set to 60 seconds. HLS media info declares audio
`hlsSegmentFormat=ts` and video `hlsVideoSegmentFormat=mpeg2_ts` to match the worker's
MPEG-TS outputs; audio-only HLS omits the video hint. `load_media_failed` on a direct
file marks subsequent bounded retries for HLS, which reuses video-copy remux or
existing NVIDIA conversion and CPU audio conversion. It does not add screen capture
or CPU video encoding. Backend polling requests have a 15-second request timeout
within the existing two-minute preparation deadline. Preparation/loading/playing
status is distinct; retries retain visible safe HTTP status/SDK error codes until
healthy playback. Arbitrary errors, content URLs and credentials are never rendered.

Playback creation has a 30-second sender deadline without aborting the create
request: a late successful response releases the created session by its ID, so
bounded waiting does not orphan a playback worker. Backend request/preparation
timeouts and network failures use fixed safe diagnostic codes.

Receiver state is read from the raw session media list by the owned content URL:
CAF's `getMediaSession()` excludes idle media and therefore hides receiver errors
and completion. A media update listener preserves those terminal states. The
active-media accessor remains the ownership guard for stopping playback, so an
old raw-media entry cannot stop another sender's current item. A ten-second
`getStatus` heartbeat has a ten-second SDK timeout; failed requests latch recovery
until a fresh receiver update or successful status request, instead of allowing
cached PLAYING state to cancel retries. Late callbacks check session/media identity.
Video loads reject receivers explicitly lacking VIDEO_OUT. The remote panel shows
inactive HDMI input state when reported by CAF. Local media elements pause on Cast
ownership, and generated MPEG-TS segment responses explicitly use `video/mp2t`.

Cast controls include a collapsed diagnostic containing the exact content URL at
the point of `loadMedia` submission, with manual copy. It includes the scoped
playback token for direct troubleshooting, stays local to the sender UI, and resets
when the playback effect restarts. It proves submission, not receiver fetch or
video output. No URL or credential is logged.


## OpenSubtitles and playback choices

The optional OpenSubtitles.com REST client uses a server-side API key and optional
bearer token from the local Viper configuration. The public capability requires
API configuration and a recent scanner heartbeat advertising provider readiness;
this describes configuration, not verified provider authentication. Typed,
authorized per-file routes expose search, download enqueue and owner-scoped job
status. Searches use TMDB IDs/year for movies and parent TMDB IDs plus season and
episode for series, with optional title override and English/Vietnamese language.
API responses contain explicit DTOs, never provider credentials or signed links.

Scanner `subtitle_download` jobs recheck file availability, account state and
library access before download and publication. A stable personal subtitle ID
(account, media file, provider file) plus a shared publication lock and conflict
handling make retries idempotent and preserve existing edited cues. Provider HTTP
calls have deadlines, bounded responses and sanitized errors. Downloads require
HTTPS OpenSubtitles domains, including redirects, without forwarding API credentials
to file hosts. ZIP/gzip extraction stays in memory; archive entries are never
written to paths from the provider. ZIP entry count and compressed size are bounded,
with 512 KiB decompressed SRT/VTT and UTF-8/UTF-16 validation. The selected subtitle
is stored in the existing owner-scoped uploaded-subtitle table. Original media
stays unchanged, so this feature does not require original subtitle editing.
Provider quotas/authentication failures use bounded job retries and the UI reports
failure. Without configuration/worker capability, the frontend hides this action.

Player choices are saved in browser storage keyed by user ID and media file ID:
audio/subtitle selection, quality, font, speed and volume. Track descriptors guard
restoration; removed uploads or changed tracks fall back rather than requesting
invalid playback. Explicit subtitle Off persists. Subtitle delay remains in the
existing server-side personal timing store. Preferences are local to the browser,
not synchronized across devices. Optional per-playback `subtitleFont` overrides
only the allowlisted libass font family; it never rewrites global encoding settings.
The Docker runtime installs DejaVu, Liberation and Noto/CJK families. Bitmap
subtitles cannot use text font styling. Both local playback and cast requests carry
the font, and changes to cast audio/subtitle/font choices reload at the receiver's
last reported position.

## Backend container layers

The single backend image still contains exactly the four application binaries.
Runtime package groups occupy separate Docker layers: pinned FFmpeg plus its
linked dependencies, MKVToolNix, Python, Latin/general text fonts and CJK fonts.
Package metadata/downloads are removed within each installation layer. Optional
Intel/AMD VAAPI/QSV/Vulkan driver modules bundled with FFmpeg are removed in that
same layer; linked dispatch/codec libraries remain installed. NVIDIA driver
libraries continue to come from Container Toolkit. Noto Sans CJK regular/bold
remain available; the unselectable Noto Serif CJK collections are omitted, while
Latin Noto Serif stays installed. Container Go binaries use `-trimpath` and
`-ldflags="-s -w"`; normal local builds retain their existing behavior.
The image sets `XDG_CACHE_HOME=/cache` for the unprivileged runtime user and installs
a fontconfig alias mapping the generic Noto Sans CJK selector to the regional
families present in Debian's TTC collections.

Smaller individual compressed blobs reduce upload duration and retry costs, but
cannot establish the cause of a registry/proxy timeout without push logs.
