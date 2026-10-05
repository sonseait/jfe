# JFE

Independent video media server with a React/Mantine frontend and Go backend.
The accepted roadmap is [PLAN.md](PLAN.md). This is the first implementation;
remaining validation and limitations are tracked in [docs/validation.md](docs/validation.md).

## Containers

Build images with the Makefiles below. Run PostgreSQL and apply migrations
before starting the API and workers. All four backend processes need the same
DATABASE_URL, JFE_MEDIA_ROOT and JFE_CACHE_ROOT, with shared read-only media
and writable cache mounts. The frontend container proxies to the API.
Backend and frontend have separate .env.example files.
The frontend image uses Caddy on port 80, with SPA fallback and a reverse proxy
for /api, /openapi.json and /docs. The default upstream is api:8090; set the
container environment variable JFE_API_UPSTREAM to override it (for example,
jfe-api:8090). Frontend and API containers must share a Docker network.

Create the initial administrator, add libraries and scan your media.
TMDB metadata is optional. API documentation is served at :8090/docs and
/openapi.json. The documentation viewer loads Scalar from a CDN.

Title details include cast names and roles from TMDB credits or local NFO
`actor` entries. Select an actor to browse their titles in your accessible
libraries. Apply migration 00006 before starting the updated backend. For
existing titles, enable **Force metadata refresh** on the library before scanning
to fetch TMDB metadata, credits and actor portraits again. TMDB must be configured;
without it, scans use local metadata. Locked titles remain unchanged. Portraits
are cached and served with library permission checks; missing images use initials.
NFO actor `thumb` supports a local image inside the configured media root.

For shows, select the library folder containing `<show>/<season>/<episode>`.
The show folder determines grouping, and season folders (`Season 02`, `S02`,
`02`, or `Specials`) override conflicting season numbers in filenames. Rescan
existing series to regroup episodes; original media files are never moved.
Series details use the full content width, with a separate Episodes section below
the cast. Episode breadcrumbs link directly back to their parent series.
Search lists movies and series only; open a series to browse its episodes.

Administrators can open **General settings** to set the server name, automatic
TMDB identification, metadata language (English/Vietnamese), actor portrait
downloads, and the watched threshold (50-100%, default 95%). Changes apply without
restart; metadata preferences affect subsequent jobs and the watched threshold
affects subsequent progress reports. Force refresh and manual identification work
when automatic lookup is off. Disabling portrait downloads keeps existing images.
TMDB controls require `TMDB_TOKEN` on both API and scanner; credentials stay in
the environment and are never returned by the settings API. The app no longer
has a Reduce motion toggle; OS/browser reduced-motion preferences still apply.

## Local development

Requires Go 1.26+, PostgreSQL 17, the golang-migrate CLI, FFmpeg/ffprobe,
Node 24 and pnpm 11.18.
For video transcoding, FFmpeg must include h264_nvenc; subtitle burn-in also
requires libass. The Debian runtime includes FFmpeg and DejaVu fonts. Test fixture
generation uses libx264; application video encoding never does.

```sh
pnpm -C frontend install --frozen-lockfile
go install -tags postgres github.com/golang-migrate/migrate/v4/cmd/migrate@v4.20.1
# Start your PostgreSQL instance and configure backend/.env first.
make -C backend migrate
# Run from the repository root in separate terminals, with the same absolute media/cache roots:
# export JFE_MEDIA_ROOT=/absolute/media JFE_CACHE_ROOT=/absolute/cache
make -C backend dev
make -C backend scanner
make -C backend downloader
make -C backend transcoder
make -C frontend dev
```

Copy `backend/.env.example` to `backend/.env` and configure DATABASE_URL there.
Backend Makefile targets load and export all variables from that file.
Running `go run ./cmd/api`, `go run ./cmd/scanner` or `go run ./cmd/transcoder`
inside `backend/` also loads `.env` automatically through Viper. Exported environment variables
override file values; a missing `.env` is allowed for container deployments.
Logging defaults to `JFE_LOG_LEVEL=info` and `JFE_LOG_FORMAT=json` on stderr.
Set `JFE_LOG_LEVEL=debug` for verbose logging or `JFE_LOG_FORMAT=console` for
human-readable development output. These settings apply to all four binaries.
The API listens on :8090; Vite proxies API
requests to it. JFE_API_PROXY overrides the development proxy target.

Migrations run directly through the `migrate` CLI (`make -C backend migrate` locally).
Existing Goose databases need a one-time version baseline before upgrading;
see [migration operations](docs/migrations.md). Fresh databases need no extra step.

## Layout and features

- `frontend/`: responsive English/Vietnamese UI, native typed client, persistent
  player, fullscreen, infinite catalog scrolling and Mantine modal scrolling.
- `backend/cmd/api`: setup/auth, users and library access, catalog, watch state,
  metadata editing, playback sessions, stream serving and administration.
- `backend/cmd/scanner`: filesystem scans, ffprobe, NFO/artwork, subtitle
  discovery, optional TMDB metadata and scheduled scans.
- `backend/cmd/transcoder`: FFmpeg remux/NVIDIA NVENC HLS, track selection, cancellation
  and playback cache cleanup.
- `backend/db`: PostgreSQL migrations, sqlc queries and generated repositories.

Native v1 targets movies and series. Music, plugins, other GPU encoders, Jellyfin API
compatibility and database import are outside scope. The frontend entry point uses `frontend/src/native`; obsolete Jellyfin modules
and dependencies have been removed.

## Checks

```sh
make -C backend lint build
make -C frontend check build
# Install sqlc v1.30.0 before regenerating contracts:
make -C backend generate
make -C frontend generate
```

Automated unit, integration and browser tests have been removed at the user's
request. CI retains build, lint and generated-contract drift checks.

Each directory has its own Makefile; run `make -C backend` or `make -C frontend`
to list targets. Backend binaries are written to `backend/bin/`. Frontend types
can also be generated from the running API with
`make -C frontend generate OPENAPI_SOURCE=http://localhost:8090/openapi.json`.
See [AGENTS.md](AGENTS.md) for development conventions.

## Container Registry

Each Makefile provides `docker-build` and `docker-push`. Push builds the image
first and stops if the build fails. Log in to your registry before pushing:

```bash
docker login ghcr.io
make -C backend docker-push REGISTRY=ghcr.io/your-org IMAGE_TAG=0.1.0
make -C frontend docker-push REGISTRY=ghcr.io/your-org IMAGE_TAG=0.1.0
```

These publish `ghcr.io/your-org/jfe-backend:0.1.0` and
`ghcr.io/your-org/jfe-frontend:0.1.0`. Override `IMAGE_NAME` to change the image
name; `IMAGE_TAG` defaults to `latest`. `REGISTRY` must include your namespace
where required (for Docker Hub, use `docker.io/your-user`).

Use `docker-build` to build locally without publishing. With no `REGISTRY`,
images are tagged `jfe-backend:latest` and `jfe-frontend:latest`.
The backend image contains all four binaries: its default command is `api`;
use the same image with command `scanner`, `downloader` or `transcoder` for the workers.

## Licensing

Frontend: GPL-3.0-only ([LICENSE](LICENSE)). Backend: AGPL-3.0-or-later
([backend/LICENSE](backend/LICENSE)), with selected media logic adapted from
Silo. See [NOTICE](NOTICE) and [backend/NOTICE](backend/NOTICE).

Library cards show available media file counts and live scan progress. After
updating an existing installation, run `make -C backend migrate` to apply
migrations through 00004, then restart the API, scanner and transcoder.
Playback quality offers Original, 720p, 1080p and 4K; resolution presets require
NVIDIA mode, the transcoder process and a working NVENC GPU. Smaller source videos are not upscaled. Selecting a quality ceiling above the source does not force video encoding. The player checks source codec support for direct playback; H.264 remux keeps video unchanged and converts only incompatible audio to AAC. Decode failures try remux before NVENC, while network errors do not force transcoding.


The subtitle menu includes Upload subtitles; saved tracks appear there immediately.
Seeking within buffered or already generated HLS segments reuses the current
session. Only positions outside that session's available media restart FFmpeg.
Bitrate measurement runs after playback is ready and does not block seeking.
HLS playback adapts prefetch to actual segment download speed: it starts at
60 seconds and targets 3 or 5 minutes after three consistently fast downloads.
A conservative 128 MiB media-byte estimate (including 30 seconds behind playback)
limits high-bitrate targets; browser memory limits may reduce buffering further.
Direct play/native HLS use `preload=auto`, with buffering controlled by the browser.
Prefetch can only load HLS segments already produced by the server.
Click the stream indicator for video/audio bitrate and codecs. Total Mbps is media
bitrate, separate from loading download speed. HLS rates are sampled from generated
output; NVENC indicates a video-encoded stream, not current GPU activity. Update
API, scanner, transcoder and frontend together, then rescan libraries once to
populate bitrate metadata for previously indexed files. No new migration is needed.

## NVIDIA transcoding

Player subtitle uploads require migration 00005: run `make -C backend migrate`
and restart the API/workers. Upload UTF-8 SRT/VTT files up to 512 KiB; they are
saved privately for the current account and media file in PostgreSQL. Back up
PostgreSQL to retain them. Subtitle timing is adjustable during playback.
Uploaded captions work with direct play/remux and do not need NVIDIA encoding.
Seek-thumbnail research is documented in [docs/seek-previews.md](docs/seek-previews.md).

Administration > Transcoding offers only **Disabled** (default) and **NVIDIA NVENC**.
Migration 00004 disables the old CPU setting, preserves concurrency and maps CRF
to the initial CQ value. Apply it with `make -C backend migrate`, then restart
all four binaries. Select NVIDIA explicitly after configuring the worker.

Disabled mode permits compatible direct play/remux. Requests needing video
encoding (resolution changes, subtitle burn-in or unsupported codecs) return
409 without creating jobs. The worker also rejects queued encoding jobs when
disabled. NVIDIA mode uses H.264 NVENC with CQ, a GPU index and concurrency
settings. GPU/driver failure fails playback; there is no CPU video fallback.
All video transcodes use NVIDIA decoding, CUDA resizing and NVENC encoding;
HDR additionally uses CUDA tone mapping.
Missing CUDA filters, GPU access or a compatible decoder/driver fail playback
with a NVIDIA-specific error. HDR never falls back to software tone mapping.
Subtitle composition and audio may still use CPU.
The system transcoding capability reflects the selected mode, not GPU health.

On a Linux NVIDIA host, install the NVIDIA driver and NVIDIA Container Toolkit.
Give the transcoder container GPU access; for example, after building the image
and configuring backend/.env with container-reachable database and /media, /cache
paths:

```sh
docker run --rm --gpus all --env-file backend/.env \
  -v /srv/media:/media:ro -v /srv/jfe-cache:/cache \
  jfe-backend:latest transcoder
```

Use the same media/cache mounts on the API and scanner; ensure /cache is writable
by the image's jfe user. Use your registry image tag if different.
The image sets NVIDIA_DRIVER_CAPABILITIES=compute,video,utility. Check
`ffmpeg -encoders` for h264_nvenc and `ffmpeg -filters` for `tonemap_cuda` and
`scale_cuda`, plus worker logs for driver/session errors. The Docker image pins
jellyfin-ffmpeg 8.1.3-1 with SHA-256 verification to provide those CUDA filters;
this is a standalone FFmpeg package, not a Jellyfin server/API dependency.
Rebuild and redeploy the backend image for this change; the old Debian FFmpeg
cannot run the new HDR path. A driver compatible with the packaged FFmpeg is required.
Source/license details are in [backend/THIRD_PARTY.md](backend/THIRD_PARTY.md).
GPU throughput and visual output require observation on the deployed NVIDIA host.
Use `nvidia-smi dmon -s u -c 5` and the session's `progress.txt` (fps/speed) to
inspect actual hardware activity and throughput.


## Audio and YouTube

JFE supports music, podcasts and audiobooks alongside movies/series. The backend
now has four application binaries: `api`, `scanner`, `transcoder`, `downloader`.
See [audio architecture and operations](docs/audio.md) for storage, permissions,
provider dependencies, tag writing, recovery and validation details.

Apply numbered migration 00007 with the external migrate CLI before restarting
all services. Generate contracts with `make -C backend generate`, followed by
`make -C frontend generate`. No application binary runs migrations.

For native workers, install `backend/requirements-audio.txt` in a Python 3.10+
virtual environment and set `JFE_PYTHON` to its interpreter. Add its `bin` directory
to PATH for `yt-dlp`; downloader also needs Node.js 20+ and FFmpeg/ffprobe.
The Docker image packages these dependencies. `JFE_IMPORT_ROOT` enables a dedicated
import directory; leaving it empty disables YouTube imports.

Video libraries remain read-only for API/transcoder; scanner also stays read-only unless original subtitle editing is explicitly enabled. Mount **audio libraries read-write for scanner**
and read-only for API/transcoder. Mount **the import directory read-write for both
scanner and downloader**, read-only for API/transcoder, at the same absolute path.
Share PostgreSQL and the cache directory among all four processes. Ensure the
worker UID/GID can create files in the import directory and replace/tag audio files.
Do not expose the media/import directories through a separate unauthenticated web server.

In the UI, create an audio library, scan it, and open an album/book/program.
Use **Edit file tags** to review selected fields before submitting file-write jobs.
Admin grants import permissions per library in user settings; that permission also
allows editing all audio files in that shared library. **Import audio** manages
YouTube sources, jobs, subscriptions and downloader settings.

On a show detail page, **Refresh metadata** opens a separate dialog offering
**Replace all metadata** (with confirmation)
or **Only episodes missing metadata**. Missing means an episode has no TMDB ID.
The latter requires an identified show and preserves show metadata and episodes
that already have a TMDB ID. Both modes respect episode metadata locks.

Home's **Pick up where you left off** rail shows the eight most recently updated
unfinished items, including show episodes. Each card displays the saved position
and a progress bar; playback progress saves refresh the rail immediately.

Video playback defaults to **Auto (network speed)** when NVENC transcoding is
enabled. It uses measured download throughput to adjust bitrate and resolution
while preserving the playback position; manual quality choices remain fixed.
Matching subtitles are selected using the interface language, including
`vi`/`vie`, Vietnamese and Tiếng Việt labels. Uploaded and embedded/sidecar subtitles are rendered into video by FFmpeg.
All subtitle playback requires NVIDIA transcoding, including SRT/WebVTT.

Video playback now prefers native direct play, then video-copy fragmented MP4
remux, then NVENC HLS. Browser support is checked against the source profile and
bit depth, including HEVC Main10. TrueHD/DTS audio can be converted to AAC without
encoding the HEVC video. HLS is used for video transcoding and legacy clients.
Apply migration **00008_playback_decision** with `make -C backend migrate`, then
rescan video libraries to refresh probe metadata and prepare text subtitles.
Later scans reuse prepared embedded text subtitles for unchanged videos; changed
files, sidecars and missing/corrupt cache are refreshed.
HDR stays copied when the client supports it. Required NVENC transcodes convert
HDR10/PQ and HLG to limited-range BT.709 SDR using `tonemap_cuda`; HDR10+ uses
its static HDR10 base. Decode, tone mapping, resize and encode stay on NVIDIA.
Subtitle rendering downloads SDR frames for CPU libass/bitmap composition and uploads
them back to the selected GPU. Single-layer Dolby Vision profile 8 with HDR10 base
compatibility ID 1 uses its HDR10 base and discards Dolby Vision metadata; other
Dolby Vision formats remain unavailable. Rescan existing libraries after upgrading
to refresh cached Dolby Vision metadata, then start a new playback session. The player
reports HDR-to-SDR conversion. See [playback architecture](docs/architecture.md).


Transcode generation is capped at 2x media time (the player's maximum speed),
with a 16-second initial burst and temporary 2.25x catch-up. Direct/remux paths
are unaffected. The worker writes `progress.txt` in the session cache with
FFmpeg `fps`/`speed` diagnostics. This caps runaway background generation, not
the CPU time needed by audio or demuxing; hardware below real-time remains a
playback bottleneck.

### Personal player subtitle timing

The player's **Subtitle timing** popover uses a slider with ±10/±60/±600-second
ranges and 0.1-second keyboard steps. Timing changes restart the NVENC session after a short debounce; releasing the
slider saves the offset to your account for that file and subtitle source. It
persists across devices and applies to FFmpeg-rendered subtitle
playback. **Use timing from file** clears the personal offset. Viewing permission
is sufficient; original files and other users' settings are unchanged. Apply
migration `00010_personal_subtitle_timing` and deploy API/frontend together.
Personal timing does not require `JFE_SUBTITLE_EDITING` or writable video mounts.

### Movie and episode subtitle editor

On a movie or episode detail page, **Manage subtitles** opens video preview, seek controls,
cue timestamps/text, and a global timing slider. Preview changes are local until
**Save to original file** is confirmed. Positive offsets show subtitles later;
negative timestamps are rejected. Sync supports UTF-8 SRT/VTT/ASS/SSA sidecars and
text tracks inside MKV. ASS preview uses plain text; original styling is preserved.
**Delete subtitle source** removes the selected MKV track (including PGS/DVD) or
sidecar for all viewers, including future burn-in playback. Image subtitle timing
sync and removing text already encoded into video pixels are unsupported. Personal
uploads can be deleted by their owner from the player's subtitle menu.

Run `make -C backend migrate` to apply migration 00009, deploy API/scanner/frontend
together, and set `JFE_SUBTITLE_EDITING=true` on **API and scanner**. Scanner requires
write permission on the original video/subtitle files and their directories; keep
API/transcoder video mounts read-only. For MKV, install `mkvmerge` and `mkvextract`
(MKVToolNix, included in the backend image). Grant video-library import/edit
permission through the user editor; administrators are already authorized.

Stop other playback sessions before saving/deleting. Scanner serializes edits
with scans, checks fingerprints, validates remux payloads/metadata and recovers
publication journals. MKV operations need space for a complete temporary remux;
no backup remains after successful publication. Preview prefers direct play/remux;
unsupported source video requires enabled NVIDIA transcoding, using a bounded
720p preview. Opening the editor stops the persistent player. Preview heartbeats
never update watched state or resume position.

See [subtitle editing architecture](docs/subtitle-editor.md) for recovery behavior
and validation boundaries.
