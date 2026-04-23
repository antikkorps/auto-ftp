package main

import (
	"bytes"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	ftpserver "github.com/fclairamb/ftpserverlib"
	"github.com/jlaffaye/ftp"
	"github.com/spf13/afero"
)

func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("grab free port: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

func startTestServer(t *testing.T, rootDir string) string {
	t.Helper()

	addr := freeAddr(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	drv := &driver{
		rootFs: afero.NewBasePathFs(afero.NewOsFs(), rootDir),
		logger: logger,
		settings: &ftpserver.Settings{
			ListenAddr:               addr,
			Banner:                   "test",
			PassiveTransferPortRange: ftpserver.PortRange{Start: 50000, End: 50050},
			IdleTimeout:              10,
		},
	}
	srv := ftpserver.NewFtpServer(drv)
	srv.Logger = logger
	if err := srv.Listen(); err != nil {
		t.Fatalf("server listen: %v", err)
	}
	go func() { _ = srv.Serve() }()
	t.Cleanup(func() { _ = srv.Stop() })

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err == nil {
			_ = c.Close()
			return addr
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatalf("server did not become reachable at %s", addr)
	return ""
}

func TestE2E_FtpPingCleanHandshake(t *testing.T) {
	addr := startTestServer(t, t.TempDir())
	if err := ftpPing(addr, 2*time.Second); err != nil {
		t.Fatalf("ftpPing: %v", err)
	}
}

func TestE2E_WrongCredentials(t *testing.T) {
	addr := startTestServer(t, t.TempDir())

	c, err := ftp.Dial(addr, ftp.DialWithTimeout(2*time.Second))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Quit()

	if err := c.Login("attacker", "attacker"); err == nil {
		t.Fatal("expected login failure with wrong credentials, got nil")
	}
}

func TestE2E_LoginAndUpload(t *testing.T) {
	root := t.TempDir()
	addr := startTestServer(t, root)

	c, err := ftp.Dial(addr, ftp.DialWithTimeout(2*time.Second))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Quit()

	if err := c.Login(ftpUser, ftpPass); err != nil {
		t.Fatalf("login: %v", err)
	}

	content := []byte("hello, auto-ftp! contenu de test avec accents éàç.")
	if err := c.Stor("report.csv", bytes.NewReader(content)); err != nil {
		t.Fatalf("STOR: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(root, "report.csv"))
	if err != nil {
		t.Fatalf("read uploaded file: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Errorf("uploaded content mismatch\ngot:  %q\nwant: %q", got, content)
	}
}

func TestE2E_PathConfinement(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Dir(root)
	addr := startTestServer(t, root)

	c, err := ftp.Dial(addr, ftp.DialWithTimeout(2*time.Second))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer c.Quit()

	if err := c.Login(ftpUser, ftpPass); err != nil {
		t.Fatalf("login: %v", err)
	}

	_ = c.Stor("../escape.txt", bytes.NewReader([]byte("must stay inside sandbox")))

	if _, err := os.Stat(filepath.Join(parent, "escape.txt")); !os.IsNotExist(err) {
		_ = os.Remove(filepath.Join(parent, "escape.txt"))
		t.Fatalf("file escaped the sandbox: found at %s", filepath.Join(parent, "escape.txt"))
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read root: %v", err)
	}
	if len(entries) == 0 {
		t.Log("STOR ../escape.txt was rejected entirely (acceptable)")
		return
	}
	for _, e := range entries {
		if e.Name() == "escape.txt" {
			return
		}
	}
	t.Errorf("unexpected files in sandbox root: %v", entries)
}
