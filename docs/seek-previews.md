# Seek thumbnail research

The player currently has time labels on its seek control. Thumbnail hover is
not enabled yet. A separate preview cache is the recommended next implementation;
opening/seeking a second video element on every mouse move would duplicate
stream traffic and can create additional HLS/NVENC sessions.

## Proposed pipeline

1. After probing a file, scanner enqueues a low-priority preview job keyed by
   file ID plus size/mtime. Scanner runs FFmpeg outside HTTP handlers with a
   timeout and cancellation; do not compete with playback transcoding jobs.
2. Extract a 160x90 image roughly every 10 seconds, letterboxing portrait and
   non-16:9 sources. Pack 100 images into each 10x10 JPEG sprite. A two-hour
   movie needs 720 thumbnails, or eight sheets. Measure real storage/CPU costs
   before enabling automatic generation for an entire library.
3. Store sprites and a manifest in the shared cache. Publish only complete
   sheets atomically; invalidate when the underlying file changes. Keep
   generation bounded and clean superseded cache versions.
4. Add typed, authenticated manifest/image routes that check library access.
   Fetch images as authenticated blobs and revoke object URLs on player close.
   Do not expose media paths or put login credentials in public cache URLs.
5. On pointer movement, map its clamped position within the seek track to
   duration, then to the manifest entry. Show a small image/time tooltip above
   the track and clamp it inside the player, including fullscreen. Prefetch only
   the current/neighboring sheets. Coalesce pointer events with animation frames.
6. On touch, show a preview while dragging; keyboard seeking keeps its time
   label. Missing/queued previews fall back to a timestamp without blocking
   playback or seeking. Respect reduced motion and avoid announcements on every
   pointer movement.

## FFmpeg feasibility

Locally verified image extraction/tiling using the installed FFmpeg:

```sh
ffmpeg -f lavfi -i testsrc2=size=320x180:rate=10 -t 12 \
  -vf 'fps=1/2,scale=160:90,tile=3x2' -frames:v 1 -update 1 preview.jpg
```

ffprobe confirms one 480x180 MJPEG image containing six 160x90 cells.
This is a synthetic feasibility check, not a benchmark on feature-length films.
Extracting/encoding still JPEG images does not encode a video stream and does
not add a CPU video-transcoding fallback. Video decoding and image processing
can use CPU under the existing policy; NVIDIA decode can be evaluated separately.

The straightforward fps filter decodes the source, which can be expensive for
4K/HDR and network storage. Keyframe-only decoding is a potential optimization,
but previews must then record actual source timestamps rather than imply exact
10-second sampling. Validate VFR sources, rotation, HDR tone mapping, source
replacement, access revocation and cache limits before shipping.
