#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
: "${JFE_TEST_DATABASE_URL:?Set JFE_TEST_DATABASE_URL to a disposable PostgreSQL instance}"
for command in migrate psql go ffmpeg ffprobe; do command -v "$command" >/dev/null; done
schemas=()
work=$(mktemp -d)
cleanup() {
  for schema in "${schemas[@]}"; do
    psql "$JFE_TEST_DATABASE_URL" -Xq -v ON_ERROR_STOP=1 -c "DROP SCHEMA IF EXISTS $schema CASCADE" >/dev/null 2>&1 || true
  done
  rm -rf "$work"
}
trap cleanup EXIT
separator='?'
if [[ "$JFE_TEST_DATABASE_URL" == *\?* ]]; then separator='&'; fi

# Each suite receives a freshly migrated schema; Go tests never run migrations.
for suite in migrations server worker; do
  schema="jfe_test_${suite}_$(date +%s)_$$"
  schemas+=("$schema")
  psql "$JFE_TEST_DATABASE_URL" -Xq -v ON_ERROR_STOP=1 -c "CREATE SCHEMA $schema"
  export JFE_TEST_SCHEMA_URL="${JFE_TEST_DATABASE_URL}${separator}search_path=$schema"
  migrate -path db/migrations -database "$JFE_TEST_SCHEMA_URL" up
  if [[ "$suite" != migrations ]]; then
    go test "./internal/$suite" -count=1
    continue
  fi

  PGOPTIONS="-c search_path=$schema" psql "$JFE_TEST_DATABASE_URL" -Xq -v ON_ERROR_STOP=1 -c "INSERT INTO settings VALUES ('sentinel', '{}')"
  migrate -path db/migrations -database "$JFE_TEST_SCHEMA_URL" up
  [[ $(PGOPTIONS="-c search_path=$schema" psql "$JFE_TEST_DATABASE_URL" -XAt -v ON_ERROR_STOP=1 -c 'SELECT version=8 AND NOT dirty FROM schema_migrations') == t ]]
  PGOPTIONS="-c search_path=$schema" psql "$JFE_TEST_DATABASE_URL" -Xq -v ON_ERROR_STOP=1 -c 'UPDATE schema_migrations SET dirty=true'
  if migrate -path db/migrations -database "$JFE_TEST_SCHEMA_URL" up >"$work/dirty.log" 2>&1; then
    echo 'Expected migrate CLI to reject a dirty schema' >&2
    exit 1
  fi
  grep -qi dirty "$work/dirty.log"
  [[ $(PGOPTIONS="-c search_path=$schema" psql "$JFE_TEST_DATABASE_URL" -XAt -v ON_ERROR_STOP=1 -c "SELECT count(*) FROM settings WHERE key='sentinel'") == 1 ]]
  PGOPTIONS="-c search_path=$schema" psql "$JFE_TEST_DATABASE_URL" -Xq -v ON_ERROR_STOP=1 -c 'UPDATE schema_migrations SET dirty=false'
  # Return to version 3 so the legacy encoding-policy migration runs again.
  migrate -path db/migrations -database "$JFE_TEST_SCHEMA_URL" down 5
  PGOPTIONS="-c search_path=$schema" psql "$JFE_TEST_DATABASE_URL" -Xq -v ON_ERROR_STOP=1 -c "UPDATE settings SET value=jsonb_build_object('threads',4,'crf',19,'maxConcurrent',3) WHERE key='encoding'"
  migrate -path db/migrations -database "$JFE_TEST_SCHEMA_URL" up
  [[ $(PGOPTIONS="-c search_path=$schema" psql "$JFE_TEST_DATABASE_URL" -XAt -v ON_ERROR_STOP=1 -c "SELECT value->>'mode'='disabled' AND (value->>'cq')::int=19 AND (value->>'maxConcurrent')::int=3 AND NOT value ? 'threads' FROM settings WHERE key='encoding'") == t ]]
  migrate -path db/migrations -database "$JFE_TEST_SCHEMA_URL" down -all
  migrate -path db/migrations -database "$JFE_TEST_SCHEMA_URL" up
  echo 'Migration CLI up/repeat/dirty/down/up checks passed'
done
