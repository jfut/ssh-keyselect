//go:build gui

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package main

import (
	"fmt"
	"strings"

	"github.com/jfut/ssh-keyselect/assets/gui"
	"github.com/jfut/ssh-keyselect/internal/branding"
	"github.com/jfut/ssh-keyselect/internal/credits"
	"github.com/jfut/ssh-keyselect/internal/guistyle"
	"github.com/jfut/ssh-keyselect/internal/transport"
	"github.com/richardwilkes/toolbox/v2/geom"
	"github.com/richardwilkes/unison"
	"github.com/richardwilkes/unison/enums/align"
	"github.com/richardwilkes/unison/enums/behavior"
	"github.com/richardwilkes/unison/enums/mod"
	"github.com/richardwilkes/unison/enums/paintstyle"
)

// The outline and content padding combine to set each status card's inner margin.
const (
	guiStatusCardBorderInset = 1
	guiStatusCardPadding     = 8
)

var (
	guiAccentInk        = unison.RGB(24, 112, 222)
	guiTextInk          = unison.RGB(28, 39, 56)
	guiMutedInk         = unison.RGB(104, 117, 134)
	guiWindowInk        = unison.RGB(245, 248, 252)
	guiCardInk          = unison.RGB(255, 255, 255)
	guiInputInk         = unison.RGB(247, 249, 252)
	guiBorderInk        = unison.RGB(220, 228, 238)
	guiMenuBorderInk    = unison.RGB(194, 209, 227)
	guiHeaderInk        = unison.RGB(241, 245, 250)
	guiKeysBadgeInk     = unison.RGB(24, 112, 222)
	guiKeysBadgeFill    = unison.RGB(232, 242, 255)
	guiActiveInk        = unison.RGB(22, 128, 74)
	guiActiveFillInk    = unison.RGB(230, 248, 238)
	guiInactiveInk      = unison.RGB(156, 96, 12)
	guiInactiveFillInk  = unison.RGB(255, 246, 224)
	guiDangerInk        = unison.RGB(182, 43, 43)
	guiDangerPressedInk = unison.RGB(145, 28, 28)
)

var guiMenuInk = unison.RGB(255, 255, 255)

// configureGUIAppearance keeps the menu and status window on the same light theme.
func configureGUIAppearance() {
	unison.ThemeSurface.Light = guiWindowInk
	unison.ThemeSurface.Dark = guiWindowInk
	unison.DefaultMenuItemTheme.BackgroundColor = guiMenuInk
	unison.DefaultMenuItemTheme.OnBackgroundColor = guiTextInk
	unison.DefaultMenuItemTheme.SelectionColor = guiAccentInk
	unison.DefaultMenuItemTheme.OnSelectionColor = guiCardInk
	unison.DefaultMenuTheme.MenuBorder = unison.NewLineBorder(guiMenuBorderInk, geom.Size{}, geom.NewUniformInsets(1), false)
	unison.DefaultMenuItemTheme.TitleFont = guiFont(11, false)
	unison.DefaultMenuItemTheme.KeyFont = guiFont(11, false)
}

// guiFont uses a small sans-serif face with on-demand fallback for missing glyphs.
func guiFont(size float32, emphasized bool) unison.Font {
	return guistyle.Font(size, emphasized)
}

// styleGUIAccentButton matches secondary actions to the prominent Refresh Keys button.
func styleGUIAccentButton(button *unison.Button) {
	guistyle.StyleAccentButton(button)
}

// styleGUIAutoSelectButton keeps the enabled state unmistakably red because it exposes every upstream key.
func styleGUIAutoSelectButton(button *unison.Button, selected, dangerous bool) {
	title := button.Text.String()
	button.Font = guiFont(9, true)
	button.SetTitle(title)
	button.HMargin = 6
	button.VMargin = 4
	button.CornerRadius = geom.NewUniformSize(5)
	button.BackgroundInk = guiCardInk
	button.OnBackgroundInk = guiTextInk
	button.EdgeInk = guiBorderInk
	button.SelectionInk = guiHeaderInk
	button.OnSelectionInk = guiTextInk
	if selected {
		if dangerous {
			button.BackgroundInk = guiDangerInk
			button.OnBackgroundInk = guiCardInk
			button.EdgeInk = guiDangerInk
			button.SelectionInk = guiDangerPressedInk
			button.OnSelectionInk = guiCardInk
		} else {
			button.BackgroundInk = guiActiveFillInk
			button.OnBackgroundInk = guiActiveInk
			button.EdgeInk = guiActiveFillInk
			button.SelectionInk = guiActiveInk
			button.OnSelectionInk = guiCardInk
		}
	}
	button.DrawCallback = func(canvas *unison.Canvas, _ geom.Rect) {
		guistyle.DrawButtonWithColors(button, canvas)
	}
	button.MarkForRedraw()
}

// showGUIAboutDialog provides a small Help menu destination with build information.
func showGUIAboutDialog() {
	content := newGUIStatusCard()
	const aboutDetailFontSize = 9.5
	header := unison.NewPanel()
	header.SetLayout(&unison.FlexLayout{Columns: 2, HSpacing: 10, VAlign: align.Middle})
	if iconPNG, err := guiassets.PNG(32); err == nil {
		if icon, imageErr := unison.NewImageFromBytes(iconPNG, geom.NewPoint(1, 1)); imageErr == nil {
			iconLabel := unison.NewLabel()
			iconLabel.Drawable = icon
			header.AddChild(iconLabel)
		}
	}
	brandDetails := unison.NewPanel()
	brandDetails.SetLayout(&unison.FlexLayout{Columns: 1, VSpacing: 2})
	name := unison.NewLabel()
	name.SetTitle(branding.Name)
	name.Font = guiFont(14, true)
	subtitle := unison.NewLabel()
	subtitle.SetTitle(branding.Subtitle)
	subtitle.Font = guiFont(10, false)
	subtitle.OnBackgroundInk = guiMutedInk
	brandDetails.AddChild(name)
	brandDetails.AddChild(subtitle)
	brandDetails.SetLayoutData(&unison.FlexLayoutData{VAlign: align.Middle})
	header.AddChild(brandDetails)
	content.AddChild(header)
	metadata := unison.NewPanel()
	metadata.SetLayout(&unison.FlexLayout{Columns: 1, VSpacing: 2})
	detail := unison.NewLabel()
	detail.Font = guiFont(aboutDetailFontSize, false)
	detail.SetTitle(fmt.Sprintf("Version: %s (%s)", version, commit))
	metadata.AddChild(detail)
	projectURL := unison.NewLabel()
	projectURL.Font = guiFont(aboutDetailFontSize, false)
	projectURL.SetTitle("Project URL: " + branding.ProjectURL)
	projectURL.OnBackgroundInk = guiMutedInk
	metadata.AddChild(projectURL)
	author := unison.NewLabel()
	author.Font = guiFont(aboutDetailFontSize, false)
	author.SetTitle("Author: " + branding.Author)
	author.OnBackgroundInk = guiMutedInk
	metadata.AddChild(author)
	content.AddChild(metadata)
	dependenciesTitle := unison.NewLabel()
	dependenciesTitle.Font = guiFont(aboutDetailFontSize, false)
	dependenciesTitle.SetTitle("Third-party libraries and licenses")
	dependenciesTitle.SetBorder(unison.NewEmptyBorder(geom.Insets{Top: 6}))
	content.AddChild(dependenciesTitle)
	dependencyText := unison.NewMultiLineField()
	dependencyText.Font = guiFont(9.5, false)
	dependencyText.NoSelectAllOnFocus = true
	dependencyText.BackgroundInk = unison.RGB(255, 255, 255)
	dependencyText.OnBackgroundInk = guiTextInk
	dependencyText.EditableInk = unison.RGB(255, 255, 255)
	dependencyText.OnEditableInk = guiTextInk
	dependencyText.SetText(strings.TrimSpace(credits.DependencyList))
	// The scroll area's rounded border replaces the field's square focus border.
	unison.UninstallFocusBorders(dependencyText, dependencyText)
	// Keep the license text clear of the rounded outline on all four sides.
	dependencyText.SetBorder(unison.NewEmptyBorder(geom.NewUniformInsets(5)))
	dependencyText.RuneTypedCallback = func(rune) bool { return true }
	dependencyText.KeyDownCallback = func(keyCode unison.KeyCode, modifiers mod.Modifiers, repeat bool) bool {
		if modifiers.OSMenuCommandDown() {
			switch keyCode {
			case unison.KeyA, unison.KeyC, unison.KeyLeft, unison.KeyRight, unison.KeyUp, unison.KeyDown:
				return dependencyText.DefaultKeyDown(keyCode, modifiers, repeat)
			default:
				return true
			}
		}
		switch keyCode {
		case unison.KeyBackspace, unison.KeyDelete, unison.KeyReturn, unison.KeyNumPadEnter:
			return true
		default:
			return dependencyText.DefaultKeyDown(keyCode, modifiers, repeat)
		}
	}
	dependencyText.RemoveCmdHandler(unison.CutItemID)
	dependencyText.RemoveCmdHandler(unison.PasteItemID)
	dependencyText.RemoveCmdHandler(unison.DeleteItemID)
	dependencyText.ContextMenuCallback = func(geom.Point) unison.Menu {
		factory := unison.DefaultMenuFactory()
		menu := factory.NewMenu(unison.PopupMenuTemporaryBaseID|unison.ContextMenuIDFlag, "", nil)
		if dependencyText.CanCopy() {
			menu.InsertItem(-1, factory.NewItem(
				unison.PopupMenuTemporaryBaseID+1|unison.ContextMenuIDFlag,
				"Copy", unison.KeyBinding{}, nil,
				func(unison.MenuItem) { dependencyText.Copy() },
			))
		}
		if dependencyText.CanSelectAll() {
			menu.InsertItem(-1, factory.NewItem(
				unison.PopupMenuTemporaryBaseID+2|unison.ContextMenuIDFlag,
				"Select All", unison.KeyBinding{}, nil,
				func(unison.MenuItem) { dependencyText.SelectAll() },
			))
		}
		if menu.Count() == 0 {
			menu.Dispose()
			return nil
		}
		return menu
	}
	dependencyScroll := unison.NewScrollPanel()
	dependencyScroll.BackgroundInk = unison.RGB(255, 255, 255)
	dependencyScroll.SetBorder(unison.NewLineBorder(guiBorderInk, geom.NewUniformSize(8), geom.NewUniformInsets(1), false))
	dependencyScroll.SetContent(dependencyText, behavior.HintedFill, behavior.Fill)
	dependencyScroll.SetLayoutData(&unison.FlexLayoutData{
		HAlign: align.Fill, VAlign: align.Fill, HGrab: true, VGrab: true, SizeHint: geom.NewSize(450, 270),
	})
	content.AddChild(dependencyScroll)
	creditsNote := unison.NewLabel()
	creditsNote.Font = guiFont(9, false)
	creditsNote.SetTitle("Full license texts are included in CREDITS.")
	creditsNote.OnBackgroundInk = guiMutedInk
	content.AddChild(creditsNote)
	if dialog, err := newGUIDialog("About "+branding.Name, nil, nil, content, []*unison.DialogButtonInfo{unison.NewOKButtonInfo()}); err == nil {
		contentRect := dialog.window.ContentRect()
		contentRect.Size.Width = max(contentRect.Size.Width, 500)
		contentRect.Size.Height = max(contentRect.Size.Height, 390)
		dialog.window.SetContentRect(contentRect)
		dialog.window.ValidateLayout()
		dependencyText.SetSelectionToStart()
		dependencyScroll.SetPosition(0, 0)
		dialog.FocusButton(0)
		dialog.RunModal()
	}
}

// newGUIStatusCard gives each section a white surface, fine outline, and generous padding.
func newGUIStatusCard() *unison.Panel {
	card := unison.NewPanel()
	card.SetLayout(&unison.FlexLayout{Columns: 1, VSpacing: 7})
	card.SetBorder(unison.NewCompoundBorder(
		unison.NewLineBorder(guiBorderInk, geom.NewUniformSize(12), geom.NewUniformInsets(guiStatusCardBorderInset), false),
		unison.NewEmptyBorder(geom.NewUniformInsets(guiStatusCardPadding)),
	))
	card.DrawCallback = func(canvas *unison.Canvas, rect geom.Rect) {
		canvas.DrawRoundedRect(rect, geom.NewUniformSize(12), guiCardInk.Paint(canvas, rect, paintstyle.Fill))
	}
	return card
}

// newGUIVerticalBar creates a colored marker aligned with the two endpoint label lines.
func newGUIVerticalBar(ink unison.Ink) *unison.Panel {
	bar := unison.NewPanel()
	// Match the height of the two endpoint label lines, including their vertical gap.
	barHeight := guiFont(10.5, false).LineHeight()*2 + 2
	bar.SetSizer(func(geom.Size) (geom.Size, geom.Size, geom.Size) {
		fixed := geom.NewSize(10, barHeight)
		return fixed, fixed, fixed
	})
	bar.SetLayoutData(&unison.FlexLayoutData{
		MinSize: geom.NewSize(10, barHeight), SizeHint: geom.NewSize(10, barHeight),
	})
	bar.DrawCallback = func(canvas *unison.Canvas, rect geom.Rect) {
		marker := geom.NewRect(rect.X, rect.Y, 3, rect.Height)
		canvas.DrawRect(marker, ink.Paint(canvas, rect, paintstyle.Fill))
	}
	return bar
}

// newGUIColoredLabel creates a text badge with the requested color and typography.
func newGUIColoredLabel(title string, fill, ink unison.Ink, fontSize float32, emphasized bool, insets geom.Insets) *unison.Panel {
	badge := unison.NewPanel()
	badge.SetLayout(&unison.FlexLayout{Columns: 1, VAlign: align.Middle})
	badge.SetBorder(unison.NewEmptyBorder(insets))
	badge.DrawCallback = func(canvas *unison.Canvas, rect geom.Rect) {
		canvas.DrawRoundedRect(rect, geom.NewUniformSize(5), fill.Paint(canvas, rect, paintstyle.Fill))
	}
	label := unison.NewLabel()
	label.Font = guiFont(fontSize, emphasized)
	label.OnBackgroundInk = ink
	label.SetTitle(title)
	label.SetLayoutData(&unison.FlexLayoutData{HAlign: align.Fill, VAlign: align.Middle})
	badge.AddChild(label)
	return badge
}

// newGUITextBadge marks a proxy endpoint with a compact green label.
func newGUITextBadge(title string) *unison.Panel {
	return newGUIColoredLabel(title, guiActiveFillInk, guiActiveInk, 7.5, false,
		geom.Insets{Top: 2, Left: 5, Bottom: 2, Right: 5})
}

// newGUISectionBadge replaces a section icon and heading with one colored text label.
func newGUISectionBadge(title string, fill, ink unison.Ink) *unison.Panel {
	return newGUIColoredLabel(title, fill, ink, 10.5, true,
		geom.Insets{Top: 3, Left: 7, Bottom: 3, Right: 7})
}

// addConnectionModeRow puts an endpoint, its copy action, and its resolved mode together.
func guiAddConnectionModeRow(card *unison.Panel, markerInk, modeFill, modeInk unison.Ink,
	title, endpointName, endpointTag, endpoint string, effective transport.Mode, modeErr error) func(string, transport.Mode, error) {
	row := unison.NewPanel()
	row.SetLayout(&unison.FlexLayout{Columns: 3, HSpacing: 10, VAlign: align.Middle})
	row.SetLayoutData(&unison.FlexLayoutData{HAlign: align.Fill, HGrab: true})

	identity := unison.NewPanel()
	identity.SetLayout(&unison.FlexLayout{Columns: 2, HSpacing: 2, VAlign: align.Middle})
	identity.AddChild(newGUIVerticalBar(markerInk))
	labels := unison.NewPanel()
	labels.SetLayout(&unison.FlexLayout{Columns: 1, VSpacing: 2, VAlign: align.Middle})
	titleLabel := unison.NewLabel()
	titleLabel.Font = guiFont(10.5, false)
	titleLabel.OnBackgroundInk = guiTextInk
	titleLabel.SetTitle(title)
	labels.AddChild(titleLabel)
	endpointDetails := unison.NewPanel()
	endpointDetails.SetLayout(&unison.FlexLayout{Columns: 2, HSpacing: 5, VAlign: align.Middle})
	endpointNameLabel := unison.NewLabel()
	endpointNameLabel.Font = guiFont(10.5, false)
	endpointNameLabel.OnBackgroundInk = guiMutedInk
	endpointNameLabel.SetTitle(endpointName)
	endpointDetails.AddChild(endpointNameLabel)
	if endpointTag != "" {
		endpointDetails.AddChild(newGUITextBadge(endpointTag))
	}
	labels.AddChild(endpointDetails)
	identity.AddChild(labels)
	identity.SetLayoutData(&unison.FlexLayoutData{
		HAlign: align.Fill, VAlign: align.Middle,
		MinSize: geom.NewSize(210, 0), SizeHint: geom.NewSize(210, 0),
	})
	row.AddChild(identity)

	endpointPanel := unison.NewPanel()
	endpointPanel.SetLayout(&unison.FlexLayout{Columns: 1, VSpacing: 0})
	pathAndCopy := unison.NewPanel()
	pathAndCopy.SetLayout(&unison.FlexLayout{Columns: 2, HSpacing: 8, VAlign: align.Middle})
	pathPanel := unison.NewPanel()
	pathPanel.SetLayout(&unison.FlexLayout{Columns: 1, VAlign: align.Middle})
	pathPanel.SetBorder(unison.NewCompoundBorder(
		unison.NewLineBorder(guiBorderInk, geom.NewUniformSize(7), geom.NewUniformInsets(1), false),
		unison.NewEmptyBorder(geom.Insets{Top: 5, Left: 8, Bottom: 5, Right: 8}),
	))
	pathPanel.DrawCallback = func(canvas *unison.Canvas, rect geom.Rect) {
		canvas.DrawRoundedRect(rect, geom.NewUniformSize(7), guiInputInk.Paint(canvas, rect, paintstyle.Fill))
	}
	pathLabel := unison.NewLabel()
	pathLabel.Font = unison.MonospacedFont.Face().Font(9.5)
	pathLabel.OnBackgroundInk = guiTextInk
	pathLabel.SetLayoutData(&unison.FlexLayoutData{SizeHint: geom.NewSize(340, 0)})
	pathPanel.AddChild(pathLabel)
	pathPanel.SetLayoutData(&unison.FlexLayoutData{HAlign: align.Fill, VAlign: align.Middle, HGrab: true})
	pathAndCopy.AddChild(pathPanel)

	copyButton := guistyle.NewIconButton(guistyle.CopyIcon, "Copy socket path")
	copyButton.BackgroundInk = guiCardInk
	copyButton.EdgeInk = guiBorderInk
	copyButton.CornerRadius = geom.NewUniformSize(7)
	copyButton.SetLayoutData(&unison.FlexLayoutData{VAlign: align.Middle})
	pathAndCopy.AddChild(copyButton)
	pathAndCopy.SetLayoutData(&unison.FlexLayoutData{HAlign: align.Fill, VAlign: align.Middle, HGrab: true})
	endpointPanel.AddChild(pathAndCopy)
	endpointPanel.SetLayoutData(&unison.FlexLayoutData{HAlign: align.Fill, VAlign: align.Middle, HGrab: true})
	row.AddChild(endpointPanel)

	modePanel := unison.NewPanel()
	modePanel.SetLayout(&unison.FlexLayout{Columns: 1, VSpacing: 3})
	modeDetails := unison.NewPanel()
	modeDetails.SetLayout(&unison.FlexLayout{Columns: 1, HSpacing: 5, VAlign: align.Middle})
	modeDetails.SetLayoutData(&unison.FlexLayoutData{VAlign: align.Middle})
	modePanel.AddChild(modeDetails)
	// Reserve the widest supported mode badge so mode changes never move the path field.
	modeBadgeWidth := guiMaxModeBadgeWidth()
	modePanel.SetLayoutData(&unison.FlexLayoutData{
		VAlign:  align.Middle,
		MinSize: geom.NewSize(modeBadgeWidth, 0), SizeHint: geom.NewSize(modeBadgeWidth, 0),
	})
	row.AddChild(modePanel)

	card.AddChild(row)
	update := func(currentEndpoint string, currentEffective transport.Mode, currentModeErr error) {
		if currentEndpoint == "" {
			pathLabel.SetTitle("Not configured")
			pathLabel.Tooltip = nil
			copyButton.SetEnabled(false)
		} else {
			displayPath := guiDisplayEndpointPath(currentEndpoint)
			pathLabel.SetTitle(guiCompactEndpoint(displayPath, 56))
			pathLabel.Tooltip = unison.NewTooltipWithText(displayPath)
			copyButton.SetEnabled(true)
			copyButton.ClickCallback = func() { unison.ClipboardSetText(displayPath) }
		}
		modeDetails.RemoveAllChildren()
		switch {
		case currentEndpoint == "":
			modeDetails.AddChild(guiNewModePill("Not configured", false, true, modeFill, modeInk, modeBadgeWidth))
		case currentModeErr != nil:
			modeDetails.AddChild(guiNewModePill("Unavailable", false, true, modeFill, modeInk, modeBadgeWidth))
		default:
			modeDetails.AddChild(guiNewModePill(guiDisplayModeName(currentEffective), true, false, modeFill, modeInk, modeBadgeWidth))
		}
		modeDetails.MarkForLayoutAndRedraw()
		row.MarkForLayoutAndRedraw()
	}
	update(endpoint, effective, modeErr)
	return update
}

func guiMaxModeBadgeWidth() float32 {
	font := guiFont(10.5, false)
	widest := float32(0)
	for _, title := range []string{
		"Automatic", "Cygwin / Git for Windows", "Unix", "WSL1", "Named Pipe (Windows OpenSSH)",
		"Not configured", "Unavailable", "unknown",
	} {
		width := unison.NewText(title, &unison.TextDecoration{Font: font}).Width()
		widest = max(widest, width)
	}
	// Reserve the measured text width, pill padding, and a small platform-metric allowance.
	return widest + 16 + 12
}

func guiCompactEndpoint(endpoint string, maxRunes int) string {
	runes := []rune(endpoint)
	if len(runes) <= maxRunes || maxRunes < 5 {
		return endpoint
	}
	remaining := maxRunes - 1
	left := remaining / 2
	right := remaining - left
	return string(runes[:left]) + "…" + string(runes[len(runes)-right:])
}

func guiDisplayModeName(mode transport.Mode) string {
	switch mode {
	case transport.Auto:
		return "Automatic"
	case transport.Unix:
		return "Unix"
	case transport.Cygwin:
		return "Cygwin / Git for Windows"
	case transport.WSL1:
		return "WSL1"
	case transport.NamedPipe:
		return "Named Pipe (Windows OpenSSH)"
	default:
		return "unknown"
	}
}

func guiNewModePill(title string, active, warning bool, activeFill, activeInk unison.Ink, badgeWidth float32) *unison.Panel {
	var fill, ink unison.Ink = guiHeaderInk, guiMutedInk
	if active {
		fill, ink = activeFill, activeInk
	} else if warning {
		fill, ink = guiInactiveFillInk, guiInactiveInk
	}
	pill := unison.NewPanel()
	pill.SetLayout(&unison.FlexLayout{Columns: 1, VAlign: align.Middle})
	pill.SetBorder(unison.NewEmptyBorder(geom.Insets{Top: 5, Left: 8, Bottom: 5, Right: 8}))
	pill.SetLayoutData(&unison.FlexLayoutData{
		HAlign:  align.Fill,
		MinSize: geom.NewSize(badgeWidth, 0), SizeHint: geom.NewSize(badgeWidth, 0),
	})
	pill.DrawCallback = func(canvas *unison.Canvas, rect geom.Rect) {
		canvas.DrawRoundedRect(rect, geom.NewUniformSize(7), fill.Paint(canvas, rect, paintstyle.Fill))
	}
	label := unison.NewLabel()
	label.Font = guiFont(10.5, false)
	label.OnBackgroundInk = ink
	// Set the font and ink before the title so the text captures the intended styling.
	label.SetTitle(title)
	labelWidth := unison.NewText(title, &unison.TextDecoration{Font: label.Font}).Width() + 12
	label.SetLayoutData(&unison.FlexLayoutData{
		HAlign: align.Fill, MinSize: geom.NewSize(labelWidth, 0), SizeHint: geom.NewSize(labelWidth, 0),
	})
	pill.AddChild(label)
	return pill
}
