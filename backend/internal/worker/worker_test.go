package worker

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"jfe/backend/internal/store"
)

func TestJobLeasesAndCancellation(t *testing.T) {
	base := os.Getenv("JFE_TEST_SCHEMA_URL")
	if base == "" {
		t.Skip("run make test-integration to prepare an isolated database schema")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	db := store.New(pool)
	enqueue := func(role string) store.Job {
		t.Helper()
		j, e := db.Enqueue(ctx, store.EnqueueParams{ID: uuid.NewString(), Role: role, Kind: role, ResourceID: uuid.NewString(), Payload: []byte("{}")})
		if e != nil {
			t.Fatal(e)
		}
		return j
	}
	expire := func(id string) {
		t.Helper()
		if _, e := pool.Exec(ctx, "UPDATE jobs SET lease_until=now()-interval '1 second' WHERE id=$1", id); e != nil {
			t.Fatal(e)
		}
	}
	state := func(id string) string {
		t.Helper()
		var value string
		if e := pool.QueryRow(ctx, "SELECT state FROM jobs WHERE id=$1", id).Scan(&value); e != nil {
			t.Fatal(e)
		}
		return value
	}
	enqueue("scanner")
	enqueue("scanner")
	type claimed struct {
		job store.Job
		err error
	}
	results := make(chan claimed, 2)
	for n := 0; n < 2; n++ {
		go func() {
			j, e := db.ClaimJob(ctx, store.ClaimJobParams{Role: "scanner", LeaseID: uuid.NewString()})
			results <- claimed{j, e}
		}()
	}
	a, b := <-results, <-results
	if a.err != nil || b.err != nil || a.job.ID == b.job.ID {
		t.Fatalf("claims overlap: %+v %+v", a, b)
	}
	if _, err = db.ClaimJob(ctx, store.ClaimJobParams{Role: "scanner", LeaseID: uuid.NewString()}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("claimed active lease: %v", err)
	}
	if _, err = db.ReportScan(ctx, store.ReportScanParams{ID: a.job.ID, LeaseID: a.job.LeaseID, TotalFiles: 10, ProcessedFiles: 4, Progress: 40}); err != nil {
		t.Fatal(err)
	}
	expire(a.job.ID)
	reclaimed, err := db.ClaimJob(ctx, store.ClaimJobParams{Role: "scanner", LeaseID: uuid.NewString()})
	if err != nil || reclaimed.ID != a.job.ID || reclaimed.Attempts != 2 {
		t.Fatalf("reclaim: %+v %v", reclaimed, err)
	}
	if reclaimed.TotalFiles != 0 || reclaimed.ProcessedFiles != 0 || reclaimed.Progress != 0 {
		t.Fatal("retry kept stale scan counters")
	}
	if _, err = db.ReportScan(ctx, store.ReportScanParams{ID: a.job.ID, LeaseID: a.job.LeaseID, TotalFiles: 10, ProcessedFiles: 5, Progress: 50}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("stale progress accepted: %v", err)
	}
	if err = db.FinishJob(ctx, store.FinishJobParams{ID: a.job.ID, LeaseID: a.job.LeaseID, State: "completed"}); err != nil {
		t.Fatal(err)
	}
	if state(a.job.ID) != "running" {
		t.Fatal("stale worker finished a new lease")
	}
	if err = db.CancelJob(ctx, reclaimed.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = db.RenewJob(ctx, store.RenewJobParams{ID: reclaimed.ID, LeaseID: reclaimed.LeaseID}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("cancelled lease renewed: %v", err)
	}
	expire(reclaimed.ID)
	if err = db.ReapJobs(ctx); err != nil {
		t.Fatal(err)
	}
	if state(reclaimed.ID) != "cancelled" {
		t.Fatal("cancelled crashed scanner remained stuck")
	}
	// Playback jobs cannot safely resume a dead FFmpeg process.
	transcode := enqueue("transcoder")
	w := Worker{DB: db, Pool: pool, Role: "transcoder"}
	active, err := w.claim(ctx)
	if err != nil || active.ID != transcode.ID {
		t.Fatalf("transcoder claim: %v", err)
	}
	enqueue("transcoder")
	if _, err = w.claim(ctx); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("encoding concurrency exceeded: %v", err)
	}
	expire(active.ID)
	if err = db.ReapJobs(ctx); err != nil {
		t.Fatal(err)
	}
	if state(active.ID) != "failed" {
		t.Fatal("expired playback job did not fail")
	}
	if _, err = w.claim(ctx); err != nil {
		t.Fatalf("expired job still occupied encoding capacity: %v", err)
	}
}
