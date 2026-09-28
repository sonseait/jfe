package worker

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	workerlog "github.com/rs/zerolog/log"
	"jfe/backend/internal/config"
	"jfe/backend/internal/store"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestYouTubePipelinePartialFailureAndDedupe(t *testing.T) {
	base := os.Getenv("JFE_TEST_SCHEMA_URL")
	if base == "" {
		t.Skip("requires disposable integration schema")
	}
	ctx := context.Background()
	pool, e := pgxpool.New(ctx, base)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	q := store.New(pool)
	python := os.Getenv("JFE_PYTHON")
	if python == "" {
		python = "python3"
	}
	fixture := filepath.Join(t.TempDir(), "source.m4a")
	if b, e := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "sine=frequency=440", "-t", "1", "-c:a", "aac", fixture).CombinedOutput(); e != nil {
		t.Fatalf("%v %s", e, b)
	}
	bin := t.TempDir()
	log := filepath.Join(bin, "calls")
	script := `import json,os,shutil,sys
args=sys.argv[1:]
if '--dump-single-json' in args:
 print(json.dumps({'title':'Test album','entries':[{'id':'abcdefghijk','title':'First'},{'id':'lmnopqrstuv','title':'Unavailable'}]}))
 sys.exit(0)
if args[-1].endswith('lmnopqrstuv'):
 print('ERROR: HTTP Error 403: Forbidden https://example.test/?signature=SECRET',file=sys.stderr)
 sys.exit(1)
with open(os.environ['JFE_FIXTURE_CALLS'],'a') as f:f.write('download\n')
path=args[args.index('--output')+1].replace('%(ext)s','m4a')
shutil.copyfile(os.environ['JFE_FIXTURE_AUDIO'],path)
with open(path.replace('.m4a','.info.json'),'w') as f:json.dump({'artist':'Fixture artist','chapters':[{'title':'Intro','start_time':0,'end_time':1}]},f)
`
	if e = os.WriteFile(filepath.Join(bin, "yt-dlp"), []byte("#!"+python+"\n"+script), 0750); e != nil {
		t.Fatal(e)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("JFE_FIXTURE_AUDIO", fixture)
	t.Setenv("JFE_FIXTURE_CALLS", log)
	user, library := uuid.NewString(), uuid.NewString()
	_, e = q.CreateUser(ctx, store.CreateUserParams{ID: user, Username: user, Role: "admin", PasswordHash: "x"})
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Exec(ctx, "DELETE FROM users WHERE id=$1", user)
	root := t.TempDir()
	l, e := q.SaveLibrary(ctx, store.SaveLibraryParams{ID: library, Name: "Audio", Kind: "music", Paths: []string{root}})
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Exec(ctx, "DELETE FROM libraries WHERE id=$1", library)
	source, e := q.CreateImportSource(ctx, store.CreateImportSourceParams{ID: uuid.NewString(), LibraryID: library, UserID: user, Url: "https://www.youtube.com/playlist?list=PLabcdefghijk"})
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Exec(ctx, "DELETE FROM jobs WHERE resource_id=$1 OR resource_id=$2", source.ID, library)
	w := Worker{DB: q, Pool: pool, Role: "downloader", Config: config.Config{MediaRoot: root, ImportRoot: t.TempDir(), CacheRoot: t.TempDir(), Python: python}}
	makeJob := func(kind string) store.Job {
		t.Helper()
		j, e := q.Enqueue(ctx, store.EnqueueParams{ID: uuid.NewString(), Role: "downloader", Kind: kind, ResourceID: source.ID, Payload: []byte("{}")})
		if e != nil {
			t.Fatal(e)
		}
		j.LeaseID = uuid.NewString()
		if _, e = pool.Exec(ctx, "UPDATE jobs SET state='running',lease_id=$2,lease_until=now()+interval '1 minute' WHERE id=$1", j.ID, j.LeaseID); e != nil {
			t.Fatal(e)
		}
		return j
	}
	preview := makeJob("youtube_preview")
	if e = w.youtubeJob(ctx, preview); e != nil {
		t.Fatal(e)
	}
	download := makeJob("youtube_download")
	var logs bytes.Buffer
	originalLogger := workerlog.Logger
	workerlog.Logger = zerolog.New(&logs)
	defer func() { workerlog.Logger = originalLogger }()
	if e = w.youtubeJob(ctx, download); e == nil {
		t.Fatal("partial failure must be reported")
	}
	entries, e := q.ListImportEntries(ctx, source.ID)
	if e != nil || len(entries) != 2 || entries[0].State != "ready" || entries[1].State != "failed" {
		t.Fatal(entries, e)
	}
	if entries[1].Error != "youtube_forbidden" {
		t.Fatalf("lost entry cause: %q", entries[1].Error)
	}
	for _, expected := range []string{download.ID, source.ID, "lmnopqrstuv", "youtube_forbidden", "exit code 1", "YouTube entry download failed"} {
		if !strings.Contains(logs.String(), expected) {
			t.Fatalf("missing log context %q: %s", expected, logs.String())
		}
	}
	if strings.Contains(logs.String(), "SECRET") || strings.Contains(logs.String(), "https://") {
		t.Fatalf("unsafe provider log: %s", logs.String())
	}
	if e = w.youtubeJob(ctx, download); e == nil {
		t.Fatal("unavailable video unexpectedly downloaded")
	}
	calls, e := os.ReadFile(log)
	if e != nil || strings.Count(string(calls), "download") != 1 {
		t.Fatal("completed file downloaded again", string(calls), e)
	}
	if e = w.scanAudio(ctx, l, func(int, int) error { return nil }); e != nil {
		t.Fatal(e)
	}
	files, e := q.LibraryFiles(ctx, l.ID)
	if e != nil || len(files) != 1 {
		t.Fatal(files, e)
	}
	meta, e := q.GetAudioMetadata(ctx, files[0].ItemID)
	if e != nil {
		t.Fatal(e)
	}
	var chapters []any
	if e = json.Unmarshal(meta.Chapters, &chapters); e != nil || len(chapters) != 1 {
		t.Fatal("chapters missing", e)
	}
	staging, e := os.ReadDir(filepath.Join(w.Config.ImportRoot, ".staging"))
	if e != nil || len(staging) != 0 {
		t.Fatal("staging not cleaned", e)
	}
}
