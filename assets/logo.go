// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

// Package assets contains the source artwork shared by the application.
package assets

import _ "embed"

// SourceLogoPNG is the embedded source artwork used to generate platform icons.
//
//go:embed ssh-keyselect-logo.png
var SourceLogoPNG []byte
