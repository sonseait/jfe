package audio

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"syscall"
)

func SyncFile(path string) error {
	f, e := os.OpenFile(path, os.O_RDWR, 0)
	if e != nil {
		return e
	}
	defer f.Close()
	return f.Sync()
}
func SyncDirectory(path string) error {
	f, e := os.Open(path)
	if e != nil {
		return e
	}
	defer f.Close()
	return f.Sync()
}
func PreserveMode(source, dest string) error {
	st, e := os.Stat(source)
	if e != nil {
		return e
	}
	if original, ok := st.Sys().(*syscall.Stat_t); ok {
		out, e := os.Stat(dest)
		if e != nil {
			return e
		}
		if target, ok := out.Sys().(*syscall.Stat_t); ok && (target.Uid != original.Uid || target.Gid != original.Gid) {
			if e = os.Chown(dest, int(original.Uid), int(original.Gid)); e != nil {
				return e
			}
		}
	}
	return os.Chmod(dest, st.Mode())
}
func WriteJournal(path string, data []byte) error {
	tmp := path + ".tmp"
	if e := os.WriteFile(tmp, data, 0600); e != nil {
		return e
	}
	if e := SyncFile(tmp); e != nil {
		return e
	}
	if e := os.Rename(tmp, path); e != nil {
		return e
	}
	return SyncDirectory(filepath.Dir(path))
}

type contextReader struct {
	ctx    context.Context
	source io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if e := r.ctx.Err(); e != nil {
		return 0, e
	}
	return r.source.Read(p)
}
func CopyContext(ctx context.Context, dest io.Writer, source io.Reader) (int64, error) {
	return io.Copy(dest, contextReader{ctx, source})
}
