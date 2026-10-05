//go:build gui

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package guistyle

import (
	"github.com/richardwilkes/toolbox/v2/geom"
	"github.com/richardwilkes/unison"
	"github.com/richardwilkes/unison/enums/mod"
)

// MakeFieldReadOnly preserves selection, navigation, and copying on every GUI surface.
func MakeFieldReadOnly(field *unison.Field) {
	field.NoSelectAllOnFocus = true
	field.RuneTypedCallback = func(rune) bool { return true }
	field.KeyDownCallback = func(keyCode unison.KeyCode, modifiers mod.Modifiers, repeat bool) bool {
		if modifiers.OSMenuCommandDown() {
			switch keyCode {
			case unison.KeyA, unison.KeyC, unison.KeyLeft, unison.KeyRight, unison.KeyUp, unison.KeyDown:
				return field.DefaultKeyDown(keyCode, modifiers, repeat)
			default:
				return true
			}
		}
		switch keyCode {
		case unison.KeyBackspace, unison.KeyDelete, unison.KeyReturn, unison.KeyNumPadEnter:
			return true
		default:
			return field.DefaultKeyDown(keyCode, modifiers, repeat)
		}
	}
	for _, command := range []int{unison.CutItemID, unison.PasteItemID, unison.DeleteItemID} {
		field.RemoveCmdHandler(command)
	}
	field.ContextMenuCallback = func(geom.Point) unison.Menu {
		factory := unison.DefaultMenuFactory()
		menu := factory.NewMenu(unison.PopupMenuTemporaryBaseID|unison.ContextMenuIDFlag, "", nil)
		if field.CanCopy() {
			menu.InsertItem(-1, factory.NewItem(
				unison.PopupMenuTemporaryBaseID+1|unison.ContextMenuIDFlag,
				"Copy", unison.KeyBinding{}, nil,
				func(unison.MenuItem) { field.Copy() },
			))
		}
		if field.CanSelectAll() {
			menu.InsertItem(-1, factory.NewItem(
				unison.PopupMenuTemporaryBaseID+2|unison.ContextMenuIDFlag,
				"Select All", unison.KeyBinding{}, nil,
				func(unison.MenuItem) { field.SelectAll() },
			))
		}
		if menu.Count() == 0 {
			menu.Dispose()
			return nil
		}
		return menu
	}
}
