//go:build !windows

package main

import "log/slog"

func acquireSingleton(_ *slog.Logger) bool    { return true }
func focusExistingWindow(_ string) bool       { return false }
func killZombieInstances(_ *slog.Logger) bool { return false }
