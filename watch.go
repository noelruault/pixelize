package pixelize

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Watch invokes onChange every time `path` is modified, until ctx is
// canceled. Coalesces bursts of events with a small debounce.
//
// Watches the parent directory and filters for the basename so editors
// that swap files atomically (write-then-rename) still trigger.
func Watch(ctx context.Context, path string, onChange func() error) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return fmt.Errorf("fsnotify: %w", err)
	}
	defer w.Close()

	dir := filepath.Dir(path)
	base := filepath.Base(path)
	if err := w.Add(dir); err != nil {
		return fmt.Errorf("watch %s: %w", dir, err)
	}

	const debounce = 100 * time.Millisecond
	var lastFire time.Time

	if err := onChange(); err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case ev, ok := <-w.Events:
			if !ok {
				return nil
			}
			if filepath.Base(ev.Name) != base {
				continue
			}
			if ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) == 0 {
				continue
			}
			if time.Since(lastFire) < debounce {
				continue
			}
			lastFire = time.Now()
			if err := onChange(); err != nil {
				return err
			}
		case err, ok := <-w.Errors:
			if !ok {
				return nil
			}
			return fmt.Errorf("watcher error: %w", err)
		}
	}
}
