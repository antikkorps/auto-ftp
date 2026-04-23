//go:build !windows

package main

import "log/slog"

func acquireSingleton(_ *slog.Logger, _ string) bool { return true }
func focusExistingWindow(_ string) bool              { return false }
