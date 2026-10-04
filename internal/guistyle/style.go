//go:build gui

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

// Package guistyle contains shared GUI typography and button styling.
package guistyle

import (
	"runtime"

	"github.com/richardwilkes/toolbox/v2/geom"
	"github.com/richardwilkes/unison"
	"github.com/richardwilkes/unison/enums/align"
	"github.com/richardwilkes/unison/enums/slant"
	"github.com/richardwilkes/unison/enums/spacing"
	"github.com/richardwilkes/unison/enums/weight"
)

var accentInk = unison.RGB(24, 112, 222)

// Font selects the application's sans-serif font with Japanese glyph coverage.
func Font(size float32, emphasized bool) unison.Font {
	family := "Noto Sans CJK JP"
	switch runtime.GOOS {
	case "windows":
		family = "Yu Gothic UI"
	case "darwin":
		family = "Hiragino Sans"
	}
	return fontFromFamily(family, size, emphasized)
}

// SymbolFont selects a monochrome symbol face instead of an emoji fallback.
func SymbolFont(size float32, emphasized bool) unison.Font {
	family := "Noto Sans Symbols 2"
	switch runtime.GOOS {
	case "windows":
		family = "Segoe UI Symbol"
	case "darwin":
		family = "Apple Symbols"
	}
	face := unison.MatchFontFace(family, fontWeight(emphasized), spacing.Standard, slant.Upright)
	if face == nil {
		return Font(size, emphasized)
	}
	return face.Font(size)
}

// StyleAccentButton applies the shared primary-action colors and padding.
func StyleAccentButton(button *unison.Button) {
	button.BackgroundInk = accentInk
	button.OnBackgroundInk = unison.RGB(255, 255, 255)
	button.EdgeInk = accentInk
	button.SelectionInk = unison.RGB(18, 88, 184)
	button.OnSelectionInk = unison.RGB(255, 255, 255)
	button.CornerRadius = geom.NewUniformSize(7)
	button.HMargin = 8
	button.VMargin = 4
}

// CenterButtonGlyph centers the glyph within a button and applies its optical baseline adjustment.
func CenterButtonGlyph(button *unison.Button, verticalMargin, baselineOffset float32) {
	button.HAlign = align.Middle
	button.VAlign = align.Middle
	button.HMargin = 7
	button.VMargin = verticalMargin
	button.Text.AdjustDecorations(func(decoration *unison.TextDecoration) {
		decoration.BaselineOffset = baselineOffset
	})
}

// SetEqualCompactIconButtonSizes gives icon-only buttons compact, matching square bounds.
func SetEqualCompactIconButtonSizes(buttons ...*unison.Button) {
	for _, button := range buttons {
		button.HMargin = 2
		button.VMargin = 0
	}
	size := geom.NewUniformSize(24)
	for _, button := range buttons {
		button.SetSizer(func(geom.Size) (geom.Size, geom.Size, geom.Size) {
			return size, size, size
		})
	}
}

// NewRefreshButton returns the icon-only refresh control shared by both GUI windows.
func NewRefreshButton() *unison.Button {
	button := unison.NewButton()
	button.Font = Font(13.5, true)
	StyleAccentButton(button)
	button.SetTitle("↻")
	CenterButtonGlyph(button, 0, 0)
	SetEqualCompactIconButtonSizes(button)
	button.DrawCallback = func(canvas *unison.Canvas, _ geom.Rect) {
		DrawAccentButtonWithWhiteGlyph(button, canvas, 0)
	}
	button.Tooltip = unison.NewTooltipWithText("Refresh Keys")
	button.SetLayoutData(&unison.FlexLayoutData{HAlign: align.End, VAlign: align.Middle})
	return button
}

// DrawAccentButtonWithWhiteGlyph draws a white symbol with an optional horizontal optical adjustment.
func DrawAccentButtonWithWhiteGlyph(button *unison.Button, canvas *unison.Canvas, horizontalGlyphOffset float32) {
	DrawButtonWithColors(button, canvas, horizontalGlyphOffset)
}

// DrawButtonWithColors draws a button using the foreground and background colors already assigned to it.
func DrawButtonWithColors(button *unison.Button, canvas *unison.Canvas, horizontalGlyphOffset float32) {
	background := button.BackgroundInk
	foreground := button.OnBackgroundInk
	if button.Pressed {
		background = button.SelectionInk
		foreground = button.OnSelectionInk
	}
	rect := button.ContentRect(false)
	thickness := float32(1)
	edge := button.EdgeInk
	if button.Focused() {
		thickness++
		edge = button.SelectionInk
	}
	unison.DrawRoundedRectBase(canvas, rect, button.CornerRadius, thickness, background, edge)
	rect = rect.Inset(geom.NewUniformInsets(thickness + 0.5))
	rect = rect.Inset(geom.NewSymmetricInsets(button.HorizontalMargin(), button.VerticalMargin()))
	rect.X += horizontalGlyphOffset
	defer button.Text.RestoreDecorations(button.Text.AdjustDecorations(func(decoration *unison.TextDecoration) {
		decoration.BackgroundInk = nil
		decoration.OnBackgroundInk = foreground
	}))
	unison.DrawLabel(canvas, rect, button.HAlign, button.VAlign, button.Font, button.Text, foreground, nil,
		button.Drawable, button.Side, button.Gap, false)
}

func fontFromFamily(family string, size float32, emphasized bool) unison.Font {
	face := unison.MatchFontFace(family, fontWeight(emphasized), spacing.Standard, slant.Upright)
	if face == nil {
		face = unison.LabelFont.Face()
	}
	return face.Font(size)
}

func fontWeight(emphasized bool) weight.Enum {
	if emphasized {
		return weight.Bold
	}
	return weight.Regular
}
