//go:build windows

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

// Package listener creates a private local endpoint for the agent frontend.
package listener

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// randomToken returns an unpredictable suffix for default agent endpoints.
func randomToken() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", fmt.Errorf("generate random agent endpoint: %w", err)
	}
	return hex.EncodeToString(bytes[:]), nil
}
