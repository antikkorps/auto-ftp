//go:build windows

package main

import (
	"log/slog"
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
)

// Held for the whole process lifetime; Windows releases the named mutex
// automatically on process exit.
var mutexHandle uintptr

// acquireSingleton returns true if we obtained the named mutex, false
// if another instance already holds it. This runs before wails.Run and
// closes a race in Wails' own SingleInstanceLock: Wails registers its
// mutex inside wails.Run, so two fast relaunches can both get past it
// before either has finished initializing. CreateMutexW is atomic at
// the Win32 level so this pre-gate is race-free.
func acquireSingleton(logger *slog.Logger, id string) bool {
	namePtr, err := syscall.UTF16PtrFromString(id)
	if err != nil {
		logger.Warn("mutex name encoding failed, skipping singleton guard", "error", err)
		return true
	}
	h, _, lastErr := procCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(namePtr)))
	if h == 0 {
		logger.Warn("CreateMutexW returned null, skipping singleton guard", "error", lastErr)
		return true
	}
	if errno, ok := lastErr.(syscall.Errno); ok && errno == errorAlreadyExists {
		// Leaving our own handle open would keep the mutex alive after
		// we exit, so the next legit launch would also see ERROR_ALREADY_EXISTS.
		procCloseHandle.Call(h)
		return false
	}
	mutexHandle = h
	return true
}

// focusExistingWindow tries to bring a window with the given title to
// the foreground, polling briefly because the racing first instance may
// still be creating its window when we run.
func focusExistingWindow(title string) bool {
	titlePtr, err := syscall.UTF16PtrFromString(title)
	if err != nil {
		return false
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		hwnd, _, _ := procFindWindowW.Call(0, uintptr(unsafe.Pointer(titlePtr)))
		if hwnd != 0 {
			procShowWindow.Call(hwnd, swRestore)
			procSetForegroundWindow.Call(hwnd)
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(150 * time.Millisecond)
	}
}
