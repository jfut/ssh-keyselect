//go:build gui

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package guistyle

import (
	"github.com/richardwilkes/toolbox/v2/geom"
	"github.com/richardwilkes/unison"
)

// Vector icons avoid loading additional font faces for UI symbols and remain
// visible even when the system's small sans-serif font lacks those glyphs.
var (
	RefreshIcon = unison.MustSVGFromContentString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path d="M12 3a9 9 0 1 0 8.5 12h-2.2A7 7 0 1 1 17 7.1L13 11h9V2l-3.5 3.5A9 9 0 0 0 12 3z"/></svg>`)
	// Center the outer teeth and the cutout at (12, 12) so the lower ring is not thinner.
	SettingsIcon = unison.MustSVGFromContentString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path fill-rule="evenodd" d="M10 2.8h4l.5 3 1.5.9 2.8-1 2 3.4-2.3 2v1.8l2.3 2-2 3.4-2.8-1-1.5.9-.5 3h-4l-.5-3-1.5-.9-2.8 1-2-3.4 2.3-2v-1.8l-2.3-2 2-3.4 2.8 1 1.5-.9z M12 8a4 4 0 1 0 0 8a4 4 0 1 0 0-8z"/></svg>`)
	CopyIcon     = unison.MustSVGFromContentString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path fill-rule="evenodd" d="M7 7h14v14H7z M9 9v10h10V9z M3 3h14v2H5v12H3z"/></svg>`)
)

// NewIconButton creates a compact control without depending on a symbol font.
func NewIconButton(icon *unison.SVG, name string) *unison.Button {
	button := unison.NewButton()
	button.Drawable = &unison.DrawableSVG{SVG: icon, Size: geom.NewUniformSize(14)}
	button.Accessibility.Name = name
	button.Tooltip = unison.NewTooltipWithText(name)
	button.DrawableOnlyHMargin = 2
	button.DrawableOnlyVMargin = 0
	button.SetSizer(func(geom.Size) (geom.Size, geom.Size, geom.Size) {
		size := geom.NewUniformSize(24)
		return size, size, size
	})
	return button
}
