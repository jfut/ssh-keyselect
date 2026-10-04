// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

// Package cmdutil holds command-line behavior shared by the CLI and GUI executables.
package cmdutil

import (
	"fmt"
	"io"
	"log/slog"
	"runtime"
	"strings"
)

// NewLogger creates a text logger for a configured command log level.
func NewLogger(w io.Writer, level string) *slog.Logger {
	var parsed slog.Level
	if strings.EqualFold(level, "off") {
		parsed = slog.LevelError + 1
	} else {
		_ = parsed.UnmarshalText([]byte(strings.ToLower(level)))
	}
	return slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{Level: parsed}))
}

// WithEnvironment replaces one environment variable, using Windows' case-insensitive key rules.
func WithEnvironment(environment []string, key, value string) []string {
	prefix := key + "="
	filtered := make([]string, 0, len(environment)+1)
	for _, entry := range environment {
		matches := strings.HasPrefix(entry, prefix)
		if runtime.GOOS == "windows" && len(entry) >= len(prefix) {
			matches = strings.EqualFold(entry[:len(prefix)], prefix)
		}
		if !matches {
			filtered = append(filtered, entry)
		}
	}
	return append(filtered, prefix+value)
}

// ReportError writes a command-prefixed error and returns the standard failure status.
func ReportError(stderr io.Writer, commandName string, err error) int {
	_, _ = fmt.Fprintf(stderr, "%s: %v\n", commandName, err)
	return 1
}
