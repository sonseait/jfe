package audio

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// Filesystem locks survive database lease expiry and are shared by all workers.
func Lock(ctx context.Context, cacheRoot, key string) (func(), error) {
	dir := filepath.Join(cacheRoot, "audio-locks")
	if e := os.MkdirAll(dir, 0750); e != nil {
		return nil, e
	}
	f, e := os.OpenFile(filepath.Join(dir, fmt.Sprintf("%x.lock", sha256.Sum256([]byte(key)))), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	for {
		if e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e == nil {
			return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }, nil
		}
		select {
		case <-ctx.Done():
			f.Close()
			return nil, ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}
