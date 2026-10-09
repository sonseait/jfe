# Build and operational verification

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
