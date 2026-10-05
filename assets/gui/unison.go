//go:build gui

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package guiassets

import (
	"slices"
	"sync"

	"github.com/richardwilkes/toolbox/v2/geom"
	"github.com/richardwilkes/unison"
)

// Share decoded images across windows; each window retains its own slice of references.
var titleIcons = sync.OnceValues(func() ([]*unison.Image, error) {
	images := make([]*unison.Image, 0, 5)
	for _, size := range []int{16, 24, 32, 48, 256} {
		iconPNG, err := PNG(size)
		if err != nil {
			return nil, err
		}
		icon, err := unison.NewImageFromBytes(iconPNG, geom.NewPoint(1, 1))
		if err != nil {
			return nil, err
		}
		images = append(images, icon)
	}
	return images, nil
})

// TitleIcons returns the shared icon sizes used in application and modal window title bars.
func TitleIcons() ([]*unison.Image, error) {
	images, err := titleIcons()
	return slices.Clone(images), err
}
