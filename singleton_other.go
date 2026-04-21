//go:build !windows

package main

import "log/slog"

func acquireSingleton(_ *slog.Logger) bool { return true }

func focusExistingWindow(_ string) {}
