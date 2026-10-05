package server

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5/pgconn"
	"jfe/backend/internal/route"
	"time"
)

// Cascading deletion can race a final playback progress transaction. PostgreSQL
// rolls the losing statement back entirely, so retry only those transient aborts.
func (s *Server) deleteLibrary(ctx context.Context, id string) error {
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		err = s.deleteLibraryWithSubtitleGuard(ctx, id)
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

func (s *Server) deleteLibraryWithSubtitleGuard(ctx context.Context, id string) error {
	tx, e := s.Pool.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	q := s.DB.WithTx(tx)
	if e = q.LockSubtitleLibrary(ctx, id); e != nil {
		return e
	}
	busy, e := q.LibrarySubtitleBusy(ctx, id)
	if e != nil {
		return e
	}
	if busy {
		return route.Fail(409, "Finish or recover original subtitle operations before removing this library")
	}
	if e = q.DeleteLibrary(ctx, id); e != nil {
		return e
	}
	return tx.Commit(ctx)
}
