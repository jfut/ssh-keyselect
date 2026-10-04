//go:build gui && !windows && !darwin && !(linux && (amd64 || arm64))

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package selector

// foregroundTarget is empty on GUI backends that do not support focus restoration.
type foregroundTarget struct{}

// capturePreviousForegroundWindow is unsupported by the remaining GUI backends.
func capturePreviousForegroundWindow() foregroundTarget { return foregroundTarget{} }

// restorePreviousForegroundWindow is unsupported by the remaining GUI backends.
func restorePreviousForegroundWindow(foregroundTarget) {}
