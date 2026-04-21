//go:build windows

package main

import (
	"log/slog"
	"syscall"
	"unsafe"
)

var (
	kernel32                = syscall.NewLazyDLL("kernel32.dll")
	user32                  = syscall.NewLazyDLL("user32.dll")
	procCreateMutexW        = kernel32.NewProc("CreateMutexW")
	procFindWindowW         = user32.NewProc("FindWindowW")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	procShowWindow          = user32.NewProc("ShowWindow")
)

const (
	errorAlreadyExists = 183
	swRestore          = 9
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
		return false
	}
	mutexHandle = h
	return true
}

func focusExistingWindow(title string) {
	titlePtr, err := syscall.UTF16PtrFromString(title)
	if err != nil {
		return
	}
	hwnd, _, _ := procFindWindowW.Call(0, uintptr(unsafe.Pointer(titlePtr)))
	if hwnd == 0 {
		return
	}
	procShowWindow.Call(hwnd, swRestore)
	procSetForegroundWindow.Call(hwnd)
}
