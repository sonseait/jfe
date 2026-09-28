# Audio architecture and operations

## Responsibilities and storage

The application has four binaries. API registers typed routes and serves all
catalog, artwork and playback data with existing authorization. Scanner owns
local audio scans, ffprobe, Mutagen tag writes and catalog reconciliation.
Downloader owns yt-dlp subprocesses, source previews, six-hour subscriptions and
publication of imported audio. Transcoder owns playback FFmpeg processes, including
CPU AAC audio conversion when video encoding is disabled.

Configure `JFE_MEDIA_ROOT`, `JFE_CACHE_ROOT` and optional `JFE_IMPORT_ROOT` identically
on the services. Video mounts stay read-only. Scanner must be able to replace audio
files and create adjacent temporary files. Downloader and scanner write the import
volume; API and transcoder only read it. Share cache because its advisory filesystem
locks coordinate workers. These locks require a local filesystem shared by the
processes; this is not a clustered deployment.

Audio libraries may point at local folders under the media root, or leave their
folder list empty for imports only. Imports are scanned
from `<import-root>/<library-id>` automatically as an additional root. They are
stored in source/video-ID directories rather than names controlled by remote metadata.
Hidden staging directories never appear in catalog scans. Original media is never
removed by source/library deletion.

## Dependencies and provider boundaries

Native workers need FFmpeg/ffprobe and a Python 3.10+ virtual environment:

```sh
python3 -m venv backend/.venv-audio
backend/.venv-audio/bin/pip install -r backend/requirements-audio.txt
export JFE_PYTHON="$PWD/backend/.venv-audio/bin/python3"
export PATH="$PWD/backend/.venv-audio/bin:$PATH"
```

Downloader also requires Node.js 20+ for yt-dlp's JavaScript challenges. The backend
Docker image includes Node, Python, Mutagen and pinned yt-dlp with its default
extras. Update the pinned downloader through an image release when YouTube changes.
Worker heartbeat advertises dependency availability; the UI hides/disables download
submission while a capable downloader is unavailable.

Only canonical YouTube video/playlist IDs are accepted. Cookies, arbitrary extractor
URLs, user-supplied command arguments, channel subscriptions and RSS are unsupported.
A local CONNECT proxy restricts yt-dlp network access to first-party YouTube hosts
and publicly routed addresses, including redirect destinations. Provider diagnostics
are classified into fixed error codes and explanatory log messages; raw stderr and
signed URLs are not retained. Process cancellation kills the
whole yt-dlp process group. FFmpeg is never run inside an HTTP request.

MusicBrainz is optional, rate-limited to one request per second per API process.
Search results fill an editable draft; they never overwrite files automatically.
Cover Art Archive artwork is fetched separately with redirect/address checks and
size/image bounds. Album artists/tags can be entered manually for AI-generated music.

## Tags and identity

MP3, FLAC, M4A/M4B, Vorbis/Opus OGG, WAV and AIFF support tag writes. Raw ADTS AAC
supports scan/playback only. The editor preserves fields not selected for change;
an empty selected field removes that tag. Multiple artists/genres use semicolons.
Artwork writes accept JPEG/PNG up to 600 KB. Local embedded covers are read during
scans, with cover.jpg/folder.jpg/cover.png as fallbacks.

The file and playable item IDs depend on library and canonical path and remain stable
through tag edits. Group identity uses the folder (the source folder for downloads),
so changing a title does not reset listening progress or change source identity.
Artist filtering stays within library authorization. Track/disc tags determine order.

An edit is a scanner job containing user, file, fingerprint and patch. Scanner
checks import permission again, rejects hardlinked/nonregular/unwritable files,
takes the shared per-file lock, copies to an adjacent temporary file, writes tags,
compares compressed audio hashes and chapters, preserves ownership/mode and fsyncs
before atomic replacement. A durable journal records the expected published inode
fingerprint. Retrying after rename reindexes instead of applying a stale edit again.
The UI reports success via the job, not merely submission. Changed source fingerprints
require reopening the editor. Original audio remains intact on verification failure.

Import permission is deliberately broad: the user can edit every audio file in the
selected library, regardless of who imported it. Read access alone never grants writes.

## Download jobs and recovery

One-time import creates a preview source; the user starts downloads after inspection.
The destination selector lists only Music, Podcasts and Audiobooks libraries with
import permission. When none are available, admins see a link to library management;
other users see guidance to request access and import permission. Video libraries
cannot be import destinations. Library loading failures show a retry action.
Following a source starts downloads automatically and checks for new entries every
six hours. Items are appended once, with independent ready/failed states. Retrying a
download skips completed videos. A video is unique within a library even if several
sources include it; source entries remain independent references.

Downloads keep the source AAC or Opus codec. WebM Opus is remuxed into an Opus file.
Titles/album/artist/order and available covers are embedded in tags. Source chapters
are stored beside the audio and included in playback metadata. No video is encoded.

Default limits are one downloader job and 5 GiB free disk space. The admin UI exposes
both settings. Space and source permissions are checked during download. Worker
claims are short transactions; active work uses renewable leases and bounded retries
with increasing delay. Staging is unique per lease. A ready marker published by atomic
directory rename permits recovery if publication succeeds but the database insert
fails. Scanner jobs are queued after downloads. Stopping subscriptions does not delete
files; there is no permanent file deletion UI in this release.

### Diagnosing failures

Downloader logs `YouTube entry download failed` with `jobId`, `sourceId`, `videoId`,
`errorCode` and the contextual error for each failed video, before reporting the
aggregate partial failure. Job-level errors include kind, resource ID and attempt.
yt-dlp errors include its exit code and a recognized diagnostic category: login/bot
verification, unavailable video, HTTP 403/429, timeout, DNS/network, TLS, missing
dependencies, unsupported format or extraction failure. Unknown provider diagnostics
remain a generic category with the process exit code; arbitrary remote text is not
logged. Storage limit and permission errors also have dedicated UI messages.

The import page and admin job page translate the same error codes. Pending retries
retain the previous error, and the import page shows a job ID for log correlation.
The failure of one video does not discard completed downloads. After updating the
downloader, retry a source to obtain new diagnostic codes; old generic job records
cannot recover error details that were previously discarded.

## Playback and validation

Audio uses the existing playback session, short-lived stream token and progress
sequence model. Browser codec/container support chooses original Range playback or
worker-generated AAC HLS. Attached cover art never counts as video. Queue state is
local per user; listening progress for long audio is stored on the server. Only one
video/audio player is active. Browser background/lock-screen support depends on Media
Session and device/browser behavior.

Run `make -C backend generate` before `make -C frontend generate`. Unit tests use
real generated audio for Mutagen round trips; set `JFE_PYTHON` or they report a skip.
`make -C backend test-integration` requires a disposable PostgreSQL URL and Mutagen;
it covers migrations, permissions, tag publication recovery and downloader leases.
`make -C frontend test-e2e` exercises the real API/scanner with desktop/mobile layouts.
Network-dependent YouTube and MusicBrainz checks are separate from deterministic
fixtures; test passes do not imply every remote video is downloadable.


## Provider verification note

The local validation attempt on 2026-09-21 reached YouTube but the public test video
required sign-in/bot verification from this environment (also reproduced directly
with yt-dlp). Public downloading on a production server is therefore not verified;
no cookie/authentication support was added. The UI reports this condition explicitly.
