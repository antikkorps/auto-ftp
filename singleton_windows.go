//go:build windows

package main

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

var (
	kernel32                = syscall.NewLazyDLL("kernel32.dll")
	user32                  = syscall.NewLazyDLL("user32.dll")
	procCreateMutexW        = kernel32.NewProc("CreateMutexW")
	procCloseHandle         = kernel32.NewProc("CloseHandle")
	procFindWindowW         = user32.NewProc("FindWindowW")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	procShowWindow          = user32.NewProc("ShowWindow")
)

const (
	errorAlreadyExists = 183
	swRestore          = 9
	createNoWindow     = 0x08000000
)

var mutexHandle uintptr

func acquireSingleton(logger *slog.Logger) bool {
	name, _ := syscall.UTF16PtrFromString("AutoFTP-EasyVIEW-v1")
	h, _, lastErr := procCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(name)))
	if h == 0 {
		logger.Warn("CreateMutexW failed, skipping singleton check", "error", lastErr)
		return true
	}
	if errno, ok := lastErr.(syscall.Errno); ok && errno == errorAlreadyExists {
		// CreateMutexW still returns a valid handle when the mutex already
		// exists. If we leave it open, our own handle keeps the named mutex
		// alive — and a retry after killing zombies will also see
		// ERROR_ALREADY_EXISTS, defeating the whole recovery.
		procCloseHandle.Call(h)
		return false
	}
	mutexHandle = h
	return true
}

func focusExistingWindow(title string) bool {
	titlePtr, err := syscall.UTF16PtrFromString(title)
	if err != nil {
		return false
	}
	hwnd, _, _ := procFindWindowW.Call(0, uintptr(unsafe.Pointer(titlePtr)))
	if hwnd == 0 {
		return false
	}
	procShowWindow.Call(hwnd, swRestore)
	procSetForegroundWindow.Call(hwnd)
	return true
}

// killZombieInstances terminates every auto-ftp.exe on this machine except
// our own PID. Named mutexes are refcounted across handles, so a single
// stray zombie keeps the lock held — we kill them all in one shot via
// taskkill and let the caller retry acquireSingleton afterwards.
func killZombieInstances(logger *slog.Logger) bool {
	exePath, err := os.Executable()
	if err != nil {
		logger.Warn("cannot determine executable path", "error", err)
		return false
	}
	exeName := filepath.Base(exePath)
	cmd := exec.Command(
		"taskkill", "/F", "/T",
		"/IM", exeName,
		"/FI", fmt.Sprintf("PID ne %d", os.Getpid()),
	)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: createNoWindow}
	out, err := cmd.CombinedOutput()
	outStr := strings.TrimSpace(string(out))
	if err != nil {
		logger.Warn("taskkill failed", "exe", exeName, "out", outStr, "error", err)
		return false
	}
	logger.Info("taskkill done", "exe", exeName, "out", outStr)
	// Give Windows a moment to release the named mutex and TCP port.
	time.Sleep(1 * time.Second)
	return true
}
