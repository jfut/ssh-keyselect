//go:build gui

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

package main

import "github.com/jfut/ssh-keyselect/internal/config"

func guiThemeLabel(theme string) string {
	if theme == config.GUIThemeDark {
		return "Dark"
	}
	return "Light"
}

func guiThemeValue(label string) string {
	if label == "Dark" {
		return config.GUIThemeDark
	}
	return config.GUIThemeLight
}
