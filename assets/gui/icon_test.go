// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package guiassets

import (
	"bytes"
	"image/png"
	"testing"
)

func TestPNGReturnsRequestedSize(t *testing.T) {
	for _, size := range []int{16, 24, 32, 48, 256} {
		data, err := PNG(size)
		if err != nil {
			t.Fatalf("PNG(%d): %v", size, err)
		}
		decoded, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatalf("decode PNG(%d): %v", size, err)
		}
		if bounds := decoded.Bounds(); bounds.Dx() != size || bounds.Dy() != size {
			t.Errorf("PNG(%d) dimensions = %dx%d", size, bounds.Dx(), bounds.Dy())
		}
	}
}
