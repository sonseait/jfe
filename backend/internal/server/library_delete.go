package server

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgconn"
	"time"
)

// Cascading deletion can race a final playback progress transaction. PostgreSQL
// rolls the losing statement back entirely, so retry only those transient aborts.
func (s *Server) deleteLibrary(ctx context.Context, id string) error {
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		err = s.DB.DeleteLibrary(ctx, id)
		var pg *pgconn.PgError
		if !errors.As(err, &pg) || (pg.Code != "40P01" && pg.Code != "40001") {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(attempt+1) * 50 * time.Millisecond):
		}
	}
	return err
}
