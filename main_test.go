package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	ftpserver "github.com/fclairamb/ftpserverlib"
)

func TestCheckCreds_Valid(t *testing.T) {
	if err := checkCreds(ftpUser, ftpPass); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestCheckCreds_WrongUser(t *testing.T) {
	if err := checkCreds("attacker", ftpPass); err == nil {
		t.Fatal("expected error for wrong user")
	}
}

func TestCheckCreds_WrongPass(t *testing.T) {
	if err := checkCreds(ftpUser, "wrong"); err == nil {
		t.Fatal("expected error for wrong password")
	}
}

func TestCheckCreds_Empty(t *testing.T) {
	if err := checkCreds("", ""); err == nil {
		t.Fatal("expected error for empty credentials")
	}
}

func TestCheckCreds_CaseSensitive(t *testing.T) {
	if err := checkCreds("VMSYNC", ftpPass); err == nil {
		t.Fatal("credential check should be case-sensitive")
	}
}

func TestBuildSettings(t *testing.T) {
	s := buildSettings()

	wantAddr := fmt.Sprintf("0.0.0.0:%d", ftpPort)
	if s.ListenAddr != wantAddr {
		t.Errorf("ListenAddr: got %q, want %q", s.ListenAddr, wantAddr)
	}

	pr, ok := s.PassiveTransferPortRange.(ftpserver.PortRange)
	if !ok {
		t.Fatalf("PassiveTransferPortRange: got %T, want ftpserver.PortRange", s.PassiveTransferPortRange)
	}
	if pr.Start != pasvStart || pr.End != pasvEnd {
		t.Errorf("PASV range: got %d-%d, want %d-%d", pr.Start, pr.End, pasvStart, pasvEnd)
	}

	if s.Banner == "" {
		t.Error("Banner should not be empty")
	}
	if s.IdleTimeout <= 0 {
		t.Error("IdleTimeout should be positive")
	}
}

func TestDriver_GetTLSConfig_Disabled(t *testing.T) {
	d := &driver{}
	cfg, err := d.GetTLSConfig()
	if cfg != nil {
		t.Errorf("expected nil TLS config, got %v", cfg)
	}
	if err == nil {
		t.Error("expected error because TLS is disabled")
	}
}

func TestEnsureGraphiquesDir_CreatesDir(t *testing.T) {
	tmp := t.TempDir()
	target := filepath.Join(tmp, folderName)

	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatalf("MkdirAll failed: %v", err)
	}

	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("target is not a directory")
	}
}

func TestLocalIPv4s_ExcludesLoopback(t *testing.T) {
	for _, ip := range localIPv4s() {
		if ip == "127.0.0.1" {
			t.Errorf("loopback 127.0.0.1 should not be in localIPv4s output")
		}
	}
}

func TestHumanizeDuration(t *testing.T) {
	tests := []struct {
		name string
		d    time.Duration
		want string
	}{
		{"under 10s", 3 * time.Second, "à l'instant"},
		{"seconds", 42 * time.Second, "il y a 42s"},
		{"minutes", 5 * time.Minute, "il y a 5 min"},
		{"hours", 3 * time.Hour, "il y a 3h"},
		{"days", 50 * time.Hour, "il y a 2j"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := humanizeDuration(tc.d)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestHumanizeDuration_Contains(t *testing.T) {
	if !strings.Contains(humanizeDuration(90*time.Second), "min") {
		t.Error("1m30s should be expressed in minutes")
	}
}

func TestShortBindError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"address in use linux", errors.New("listen tcp 0.0.0.0:2121: bind: address already in use"), fmt.Sprintf("PORT %d DÉJÀ UTILISÉ", ftpPort)},
		{"address in use windows", errors.New("listen tcp 0.0.0.0:2121: bind: Only one usage of each socket address is normally permitted."), fmt.Sprintf("PORT %d DÉJÀ UTILISÉ", ftpPort)},
		{"permission denied linux", errors.New("listen tcp 0.0.0.0:21: bind: permission denied"), "PERMISSION REFUSÉE"},
		{"permission denied windows", errors.New("listen tcp 0.0.0.0:21: bind: access is denied."), "PERMISSION REFUSÉE"},
		{"unknown error", errors.New("something completely different"), "DÉMARRAGE IMPOSSIBLE"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := shortBindError(tc.err)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
