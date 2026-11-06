package worker

import (
	"context"
	"errors"
	"os/exec"
)

func remuxDownloaded(ctx context.Context, src, dest string) error {
	if e := exec.CommandContext(ctx, "ffmpeg", "-nostdin", "-v", "error", "-i", src, "-map", "0:a:0", "-c:a", "copy", "-vn", dest).Run(); e != nil {
		return errors.New("downloaded audio remux failed")
	}
	return nil
}
