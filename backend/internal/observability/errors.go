package observability

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/rs/zerolog"
	"jfe/backend/internal/audio"
	"jfe/backend/internal/subtitleprovider"
)

// Error adds typed diagnostics without SQL values, command arguments or raw
// provider bodies. User-facing error/DTO contracts are deliberately unchanged.
func Error(event *zerolog.Event, err error) *zerolog.Event {
	event.Err(err)
	var provider *subtitleprovider.Error
	if errors.As(err, &provider) {
		event.Object("provider", provider)
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) {
		event.Str("sqlState", pg.Code).Str("table", pg.TableName).Str("constraint", pg.ConstraintName)
	}
	var path *os.PathError
	if errors.As(err, &path) {
		event.Str("fileOperation", path.Op).Str("filename", filepath.Base(path.Path))
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		event.Int("exitCode", exit.ExitCode())
	}
	var youtube *audio.YouTubeError
	if errors.As(err, &youtube) {
		event.Str("provider", "youtube").Str("reason", youtube.Reason).Int("exitCode", youtube.ExitCode)
	}
	return event
}
