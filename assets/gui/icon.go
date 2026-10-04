// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

// Package guiassets embeds the source artwork and creates the icon sizes used by the GUI.
package guiassets

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/png"

	"github.com/jfut/ssh-keyselect/assets"
	"golang.org/x/image/draw"
)

var sourcePNG = assets.SourceLogoPNG

// PNG returns the source icon scaled to the requested square size.
func PNG(size int) ([]byte, error) {
	if size < 1 {
		return nil, errors.New("icon size must be positive")
	}
	if size == 256 {
		return bytes.Clone(sourcePNG), nil
	}

	source, err := png.Decode(bytes.NewReader(sourcePNG))
	if err != nil {
		return nil, fmt.Errorf("decode source icon: %w", err)
	}
	resized := image.NewNRGBA(image.Rect(0, 0, size, size))
	draw.CatmullRom.Scale(resized, resized.Bounds(), source, source.Bounds(), draw.Src, nil)

	var encoded bytes.Buffer
	if err := png.Encode(&encoded, resized); err != nil {
		return nil, fmt.Errorf("encode %dpx icon: %w", size, err)
	}
	return encoded.Bytes(), nil
}
