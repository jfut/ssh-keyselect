//go:build gui

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package main

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/jfut/ssh-keyselect/internal/config"
)

// guiLogOutput keeps logger handlers stable while Settings changes their destination.
type guiLogOutput struct {
	mu       sync.Mutex
	fallback io.Writer
	file     *os.File
}

func newGUILogOutput(fallback io.Writer, path string) (*guiLogOutput, error) {
	output := &guiLogOutput{fallback: fallback}
	file, err := output.open(path)
	if err != nil {
		return nil, err
	}
	output.file = file
	return output, nil
}

// newGUILogger shares a live level with the proxy and UI while keeping one destination writer.
func newGUILogger(output io.Writer, level *slog.LevelVar, configured string) *slog.Logger {
	level.Set(guiSlogLevel(configured))
	return slog.New(slog.NewTextHandler(output, &slog.HandlerOptions{Level: level}))
}

func guiSlogLevel(configured string) slog.Level {
	switch strings.ToLower(configured) {
	case "off":
		return slog.LevelError + 1
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func (o *guiLogOutput) open(path string) (*os.File, error) {
	path = config.ExpandPath(path)
	if path == "" {
		return nil, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, fmt.Errorf("create log directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return nil, fmt.Errorf("open log file: %w", err)
	}
	if err := file.Chmod(0600); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("set log file permissions: %w", err)
	}
	return file, nil
}

func (o *guiLogOutput) replace(file *os.File) {
	o.mu.Lock()
	previous := o.file
	o.file = file
	o.mu.Unlock()
	if previous != nil {
		_ = previous.Close()
	}
}

func (o *guiLogOutput) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.file != nil {
		return o.file.Write(p)
	}
	return o.fallback.Write(p)
}

func (o *guiLogOutput) Close() error {
	o.mu.Lock()
	file := o.file
	o.file = nil
	o.mu.Unlock()
	if file != nil {
		return file.Close()
	}
	return nil
}
