//go:build gui

// SPDX-License-Identifier: Apache-2.0
// SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

// Package credits embeds the compact dependency license list shown in About.
package credits

import _ "embed"

// DependencyList is regenerated from all supported platform builds by `just deps-credits`.
//
//go:embed dependencies.txt
var DependencyList string
