package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

func humanizeDuration(d time.Duration) string {
	switch {
	case d < 10*time.Second:
		return "à l'instant"
	case d < time.Minute:
		return fmt.Sprintf("il y a %ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("il y a %d min", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("il y a %dh", int(d.Hours()))
	default:
		return fmt.Sprintf("il y a %dj", int(d.Hours()/24))
	}
}

type activityTracker struct {
	mu       sync.Mutex
	lastFile string
	lastAt   time.Time
}

func (a *activityTracker) record(name string) {
	a.mu.Lock()
	a.lastFile = name
	a.lastAt = time.Now()
	a.mu.Unlock()
}

func (a *activityTracker) snapshot() (string, time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.lastFile, a.lastAt
}

func watchFolder(ctx context.Context, dir string, logger *slog.Logger, onEvent func(name string)) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		logger.Error("fsnotify init failed", "error", err)
		return
	}
	if err := w.Add(dir); err != nil {
		logger.Error("fsnotify add failed", "dir", dir, "error", err)
		_ = w.Close()
		return
	}
	go func() {
		defer w.Close()
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-w.Events:
				if !ok {
					return
				}
				if ev.Op&(fsnotify.Create|fsnotify.Write) != 0 {
					onEvent(filepath.Base(ev.Name))
				}
			case err, ok := <-w.Errors:
				if !ok {
					return
				}
				logger.Warn("fsnotify error", "error", err)
			}
		}
	}()
}

func heartbeat(ctx context.Context, addr string, logger *slog.Logger, onFail func(err error), onOK func()) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
			if err != nil {
				logger.Warn("heartbeat failed", "addr", addr, "error", err)
				onFail(err)
				continue
			}
			_ = conn.Close()
			onOK()
		}
	}
}
