//go:build gui

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package selector

import (
	"context"
	"errors"
	"testing"

	"github.com/jfut/ssh-keyselect/internal/identity"
)

func TestStoppedGUISelectorCancelsWithoutStartingTheUI(t *testing.T) {
	gui := NewGUISelector()
	gui.Stop()
	_, err := gui.Select(context.Background(), []identity.Identity{{Comment: "key"}}, SelectionContext{})
	if !errors.Is(err, ErrCancelled) {
		t.Fatalf("Select error = %v, want cancellation", err)
	}
}
