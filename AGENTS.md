# JFE

JFE is becoming an independent video media server. PLAN.md is the accepted scope.
The frontend is in frontend/; the backend is in backend/. Reference Silo at
../silo-server for selected media logic, not as a dependency or a tree
to copy. Preserve attribution and AGPL licensing of adapted backend code.

## Architecture

- Frontend: React, Mantine, TypeScript, react-hot-toast, react-router-dom,
  react-i18next, Lucide, TanStack Query and Zustand. Keep the existing cinematic
  design, thin icons, Mantine ScrollArea modals and persistent player.
- Backend: Go, Fiber v3, pgx/v5, sqlc, PostgreSQL, rs/zerolog.
- Exactly four application binaries: api, scanner, transcoder, downloader. OpenAPI export
  is an api subcommand. Database migrations run through the external migrate CLI.
  Workers share PostgreSQL and media/cache volumes.
- api owns HTTP, access control and file serving. scanner owns scan/probe,
  metadata, audio tag writes and scan scheduling. transcoder owns FFmpeg playback processes.
  downloader owns YouTube preview/download and subscription scheduling.
- Product endpoints go through the typed route registration wrapper. Body,
  query, params and response have DTOs; Empty represents unused input. Route
  registration also generates OpenAPI. Do not expose database models as DTOs.
  Use route.Get/Post/Put/Patch/Delete wrappers over Fiber, not Huma. DTO jsonschema
  tags drive both schema generation (invopop/jsonschema) and request validation
  (santhosh-tekuri/jsonschema). The application owns route registration/OpenAPI.
- Use a local Viper instance to read backend .env and environment variables.
  Environment values take precedence, including explicitly empty values. Do not
  add a separate dotenv loader or mutate the process environment during loading.
- Services must not retain Fiber contexts or depend on HTTP handlers.
- SQL lives in backend/db/queries and migrations; run sqlc generate after edits.
  Use the official golang-migrate CLI with numbered .up.sql/.down.sql pairs.
  Do not embed a migration library/runner in the application or automatically
  force dirty/legacy databases. Integration schema setup belongs in shell scripts,
  not Go helpers. Compose uses the official migrate/migrate image.
  Never edit generated query code or generated frontend API types by hand.
- Every API addition/change includes its frontend integration in the
  same change. Capability-gate unsupported features; do not leave dead controls.

## Product and safety

V1 targets movies/series plus music, podcasts and audiobooks, local metadata, TMDB
and optional MusicBrainz, independent admin/user accounts, direct play/remux and
NVIDIA NVENC HLS. Video mounts remain read-only for api/transcoder. Scanner may
write video directories only for explicitly enabled original subtitle sync/deletion
(`JFE_SUBTITLE_EDITING`); publish through leased jobs with locks, fingerprints and
recovery journals, without video/audio encoding. Audio mounts must be writable by
scanner for explicit tag edits. The import volume is writable by scanner/downloader
and read-only for api/transcoder. Video
transcoding has exactly two modes: nvidia or disabled (the default). Never add
a software video encoder or fall back to CPU video encoding. Audio encoding,
decoding and filters can use CPU. No household profiles, Jellyfin
compatibility, other GPU backends, plugins, Redis, S3 or cluster in v1.
The frontend uses the native API only. Production must never use fixture responses.

Enforce library permissions on catalog, images and streams. Hash login tokens,
use short-lived playback credentials, and never log secrets. Resolve media paths
inside configured roots. Removing a library must not delete original media.
Jobs use short claim transactions, leases and bounded retries. Do not run FFmpeg
inside HTTP requests. Clean up playback processes, HLS resources and observers.
Preserve settings that are not edited. Confirm destructive UI actions.
All visible frontend text supports English and Vietnamese.
Users with import permission may edit all audio files in the granted library.
Write tags through scanner jobs: preserve unedited tags and audio, use per-file
locks and atomic replacement, verify fingerprints and recover publication journals.
YouTube uses public URLs only, append-only subscriptions, yt-dlp via downloader
and configured writable import storage. Never overwrite edited imported files.

## Validation

Use backend/Makefile and frontend/Makefile via make -C backend or make -C frontend.
Generate backend contracts before frontend types. Automated testing has been
removed at the user's request. Do not add or run unit, integration, media fixture
or browser test suites unless the user explicitly asks to restore them.
Keep builds, lint and contract generation. Preserve runtime request validation,
authorization and recovery behavior.
Update README and durable architecture/validation docs when behavior changes.
State unverified real-device/server behavior accurately. PLAN.md records the
accepted plan; implementation progress belongs in separate documentation.
