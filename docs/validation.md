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
