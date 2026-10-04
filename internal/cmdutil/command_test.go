// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package cmdutil

import (
	"bytes"
	"testing"
)

func TestNewLoggerOffSuppressesMessages(t *testing.T) {
	var output bytes.Buffer
	logger := NewLogger(&output, "off")
	logger.Info("suppressed")
	logger.Error("also suppressed")
	if output.Len() != 0 {
		t.Fatalf("logger output with level off = %q, want empty", output.String())
	}
}
