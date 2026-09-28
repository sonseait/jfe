#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
command -v migrate >/dev/null || { echo 'Install the golang-migrate CLI; see docs/migrations.md' >&2; exit 1; }
work=$(mktemp -d "${TMPDIR:-/tmp}/jfe-e2e.XXXXXX")
schema="jfe_e2e_$(date +%s)_$$"
base="${JFE_TEST_DATABASE_URL:-postgres://jfe:jfe@localhost:55432/jfe?sslmode=disable}"
pids=()
cleanup() {
  for pid in "${pids[@]}"; do kill "$pid" 2>/dev/null || true; done
  for pid in "${pids[@]}"; do wait "$pid" 2>/dev/null || true; done
  psql "$base" -q -c "DROP SCHEMA IF EXISTS $schema CASCADE" >/dev/null 2>&1 || true
  echo "E2E process logs and media: $work"
}
trap cleanup EXIT
psql "$base" -q -c "CREATE SCHEMA $schema"
export DATABASE_URL="${base}$([[ "$base" == *\?* ]] && echo '&' || echo '?')search_path=$schema"
export JFE_IMPORT_ROOT="$work/imports"
export JFE_MEDIA_ROOT="$work/media" JFE_CACHE_ROOT="$work/cache" JFE_LISTEN=127.0.0.1:18090
export JFE_API_PROXY=http://127.0.0.1:18090 JFE_TEST_MEDIA_ROOT="$work/media" JFE_TEST_PORT=13000
export JFE_TEST_BUFFER_FIXTURE="$work/buffer.mp4"
mkdir -p "$work/media" "$work/cache" "$work/imports" "$work/media/Music"
ffmpeg -v error -f lavfi -i sine=frequency=330 -t 30 -c:a flac -metadata title="Audio Fixture" -metadata album="Test Album" -metadata artist="Test Artist" "$work/media/Music/01.flac"
ffmpeg -v error -f lavfi -i testsrc=size=160x90:rate=5 -t 420 -c:v libx264 -preset ultrafast -pix_fmt yuv420p -g 20 -movflags +faststart "$JFE_TEST_BUFFER_FIXTURE"
ffmpeg -v error -f lavfi -i testsrc=size=320x180:rate=24 -f lavfi -i sine=frequency=440 -t 30 -c:v libx264 -pix_fmt yuv420p -c:a aac -movflags +faststart "$work/media/Quiet Horizon (2025).mp4"
ffmpeg -v error -i "$work/media/Quiet Horizon (2025).mp4" -frames:v 1 "$work/media/actor.jpg"
cat > "$work/media/Quiet Horizon (2025).nfo" <<'EOF'
<movie><actor><name>Test Actor</name><role>Lead</role><tmdbid>42</tmdbid><thumb>actor.jpg</thumb></actor><actor><name>An Actor With A Very Long Display Name</name><role>Supporting</role><thumb>actor.jpg</thumb></actor></movie>
EOF
(cd backend && go build -o "$work/api" ./cmd/api && go build -o "$work/scanner" ./cmd/scanner && go build -o "$work/transcoder" ./cmd/transcoder)
migrate -path backend/db/migrations -database "$DATABASE_URL" up
for binary in api scanner transcoder; do "$work/$binary" >"$work/$binary.log" 2>&1 & pids+=("$!"); done
for attempt in $(seq 1 50); do if curl -fsS http://127.0.0.1:18090/api/v1/health >/dev/null; then break; fi; sleep 0.1; done
pnpm -C frontend exec playwright test "$@"
