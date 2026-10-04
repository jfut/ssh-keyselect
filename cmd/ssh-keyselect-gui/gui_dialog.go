//go:build gui

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package main

import (
	"slices"

	"github.com/jfut/ssh-keyselect/assets/gui"
	"github.com/jfut/ssh-keyselect/internal/branding"
	"github.com/richardwilkes/toolbox/v2/geom"
	"github.com/richardwilkes/unison"
	"github.com/richardwilkes/unison/enums/align"
	"github.com/richardwilkes/unison/enums/mod"
)

// guiDialog is a modal application window that does not stay above unrelated windows.
type guiDialog struct {
	window  *unison.Window
	buttons []*unison.Button
}

const guiDialogButtonGap = 9

func newGUIDialog(title string, icon unison.Drawable, iconInk unison.Ink, body unison.Paneler, buttons []*unison.DialogButtonInfo) (*guiDialog, error) {
	window, err := unison.NewWindow(title, unison.WindowKindWindowOption(unison.WindowKindDialog))
	if err != nil {
		return nil, err
	}
	if icons, iconErr := guiassets.TitleIcons(); iconErr == nil {
		window.SetTitleIcons(icons)
	}

	content := window.Content()
	content.SetBorder(unison.NewEmptyBorder(geom.NewUniformInsets(guiStatusCardBorderInset + guiStatusCardPadding)))
	columns := 1
	if icon != nil {
		columns++
		iconLabel := unison.NewLabel()
		iconLabel.Drawable = icon
		iconLabel.OnBackgroundInk = iconInk
		iconLabel.SetBorder(unison.NewEmptyBorder(geom.Insets{Bottom: 2 * unison.StdHSpacing, Right: unison.StdHSpacing}))
		iconLabel.SetLayoutData(&unison.FlexLayoutData{})
		content.AddChild(iconLabel)
	}
	content.SetLayout(&unison.FlexLayout{
		Columns:  columns,
		HSpacing: unison.StdHSpacing,
		VSpacing: unison.StdVSpacing,
	})
	bodyPanel := body.AsPanel()
	bodyBottomInset := geom.Insets{Bottom: guiDialogButtonGap - unison.StdVSpacing}
	bodyPanel.SetLayoutData(&unison.FlexLayoutData{
		HSpan:  1,
		VSpan:  1,
		HAlign: align.Fill,
		VAlign: align.Fill,
		HGrab:  true,
		VGrab:  true,
	})
	// Keep button spacing outside the body so its painted background stays within its border.
	bodyContainer := unison.NewPanel()
	bodyContainer.SetLayout(&unison.FlexLayout{Columns: 1})
	bodyContainer.SetBorder(unison.NewEmptyBorder(bodyBottomInset))
	bodyContainer.SetLayoutData(&unison.FlexLayoutData{
		HSpan:  1,
		VSpan:  1,
		HAlign: align.Fill,
		VAlign: align.Fill,
		HGrab:  true,
		VGrab:  true,
	})
	bodyContainer.AddChild(bodyPanel)
	content.AddChild(bodyContainer)

	buttonPanel := unison.NewPanel()
	buttonPanel.SetLayout(&unison.FlexLayout{
		Columns:      len(buttons) + 1,
		HSpacing:     unison.StdHSpacing * 2,
		VSpacing:     unison.StdVSpacing,
		EqualColumns: true,
	})
	buttonPanel.AddChild(unison.NewPanel())
	buttonWidgets := make([]*unison.Button, 0, len(buttons))
	for _, info := range buttons {
		button := unison.NewButton()
		// Set the font before the title so its text uses the regular GUI face.
		button.Font = guiFont(11.5, false)
		button.SetTitle(info.Title)
		button.ClickCallback = func() { window.StopModal(info.ResponseCode) }
		button.SetLayoutData(&unison.FlexLayoutData{HSpan: 1, VSpan: 1, HAlign: align.Fill, VAlign: align.Middle})
		buttonPanel.AddChild(button)
		buttonWidgets = append(buttonWidgets, button)
	}
	buttonPanel.SetLayoutData(&unison.FlexLayoutData{
		HSpan:  columns,
		VSpan:  1,
		HAlign: align.End,
		VAlign: align.Middle,
	})
	content.AddChild(buttonPanel)

	originalKeyDownCallback := content.KeyDownCallback
	content.KeyDownCallback = func(keyCode unison.KeyCode, modifiers mod.Modifiers, repeat bool) bool {
		ok := false
		if originalKeyDownCallback == nil {
			ok = true
		} else {
			unison.SafeCall(func() { ok = !originalKeyDownCallback(keyCode, modifiers, repeat) })
		}
		if !ok {
			return true
		}
		if modifiers&mod.NonSticky == 0 {
			for index, info := range buttons {
				if !slices.Contains(info.KeyCodes, keyCode) {
					continue
				}
				if buttonWidgets[index].Enabled() {
					buttonWidgets[index].Click()
				}
				return true
			}
		}
		return false
	}
	window.AllowCloseCallback = func() bool {
		window.StopModal(unison.ModalResponseCancel)
		return false
	}
	window.Pack()
	window.MoveToModalCenter(unison.ActiveWindow())
	return &guiDialog{window: window, buttons: buttonWidgets}, nil
}

func (d *guiDialog) RunModal() int { return d.window.RunModal() }

// FocusButton directs initial focus to a dialog action instead of an editable body field.
func (d *guiDialog) FocusButton(index int) {
	if d != nil && index >= 0 && index < len(d.buttons) {
		d.window.SetFocus(d.buttons[index])
	}
}

// showGUIErrorDialog displays errors without pinning the alert above unrelated applications.
func showGUIErrorDialog(message string, err error) {
	detail := ""
	if err != nil {
		detail = err.Error()
	}
	dialog, createErr := newGUIDialog(
		branding.Name,
		unison.DefaultDialogTheme.ErrorIcon,
		unison.DefaultDialogTheme.ErrorIconInk,
		unison.NewMessagePanel(message, detail),
		[]*unison.DialogButtonInfo{unison.NewOKButtonInfo()},
	)
	if createErr == nil {
		dialog.RunModal()
	}
}

// confirmAutoSelectEnable explains the access granted by Auto Select before enabling it.
func guiConfirmAutoSelectEnable() bool {
	message := unison.NewMessagePanel(
		"Every upstream identity will be available to clients using this proxy.",
		"Any client that can access this proxy can request signatures with any loaded key.\n\nUse this only for trusted batch work.\n\nTurn Auto Select Off when finished.",
	)
	// Keep the warning headline emphasized while using the regular app font for its explanation.
	for index, child := range message.Children() {
		if index == 0 {
			continue
		}
		if label, ok := child.Self.(*unison.Label); ok {
			title := label.String()
			label.Font = guiFont(10, false)
			label.SetTitle(title)
		}
	}
	dialog, err := newGUIDialog(
		"Enable Auto Select?",
		unison.DefaultDialogTheme.WarningIcon,
		unison.DefaultDialogTheme.WarningIconInk,
		message,
		[]*unison.DialogButtonInfo{
			unison.NewCancelButtonInfo(),
			{Title: "On", ResponseCode: unison.ModalResponseOK},
		},
	)
	if err != nil {
		return false
	}
	// Start on Cancel, and require an explicit click on On to enable pass-through.
	dialog.FocusButton(0)
	return dialog.RunModal() == unison.ModalResponseOK
}
