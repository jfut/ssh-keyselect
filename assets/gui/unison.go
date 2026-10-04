//go:build gui

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package guiassets

import (
	"github.com/richardwilkes/toolbox/v2/geom"
	"github.com/richardwilkes/unison"
)

// TitleIcons decodes the icon sizes used in application and modal window title bars.
func TitleIcons() ([]*unison.Image, error) {
	var images []*unison.Image
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
}
