package main

import (
	"bufio"
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
			if err := ftpPing(addr, 2*time.Second); err != nil {
				logger.Warn("heartbeat failed", "addr", addr, "error", err)
				onFail(err)
				continue
			}
			onOK()
		}
	}
}

// ftpPing opens a TCP connection and completes a banner/QUIT exchange
// so ftpserverlib records a clean client lifecycle. A plain Dial+Close
// was enough to prove the listener is alive but ftpserverlib would
// then log the abrupt disconnect at ERROR level every 30 s.
func ftpPing(addr string, timeout time.Duration) error {
	conn, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return err
	}
	br := bufio.NewReader(conn)
	if _, err := br.ReadString('\n'); err != nil {
		return err
	}
	if _, err := conn.Write([]byte("QUIT\r\n")); err != nil {
		return err
	}
	if _, err := br.ReadString('\n'); err != nil {
		return err
	}
	return nil
}
