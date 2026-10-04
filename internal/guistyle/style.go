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

// Keep the smaller font families close to the previous CJK fonts' visual size.
const fontSizeScale = float32(0.9)

// ConfigureFonts also scales widgets that use Unison's default fonts, such as
// settings fields, tooltips, and messages, before their text is created.
func ConfigureFonts() {
	regular := Font(10, false)
	unison.SystemFont.Font = regular
	unison.EmphasizedSystemFont.Font = Font(10, true)
	unison.LabelFont.Font = regular
	unison.FieldFont.Font = regular
	unison.KeyboardFont.Font = regular
	unison.MonospacedFont.Font = MonospacedFont(10)
}

// Font keeps the mostly Latin UI on a small sans-serif face. Unison loads fallback
// faces for missing glyphs when needed, instead of retaining CJK faces at startup.
func Font(size float32, emphasized bool) unison.Font {
	family := "Noto Sans"
	switch runtime.GOOS {
	case "windows":
		family = "Segoe UI"
	case "darwin":
		family = "Helvetica Neue"
	}
	return fontFromFamily(family, size*fontSizeScale, emphasized)
}

// MonospacedFont applies the same compact size to paths and selection details.
func MonospacedFont(size float32) unison.Font {
	return unison.MonospacedFont.Face().Font(size * fontSizeScale)
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

// NewRefreshButton returns the icon-only refresh control shared by both GUI windows.
func NewRefreshButton() *unison.Button {
	button := NewIconButton(RefreshIcon, "Refresh Keys")
	StyleAccentButton(button)
	button.SetLayoutData(&unison.FlexLayoutData{HAlign: align.End, VAlign: align.Middle})
	return button
}

// DrawButtonWithColors draws a button using the foreground and background colors already assigned to it.
func DrawButtonWithColors(button *unison.Button, canvas *unison.Canvas) {
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
