#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

resource_dir="assets/gui/generated/windows"
mkdir -p "$resource_dir"

# Keep each executable's icon and file properties in one object for the Go linker.
go run github.com/tc-hib/go-winres@v0.3.3 make \
	--in assets/gui/windows-cli-resources.json \
	--arch amd64,arm64 \
	--out "$resource_dir/rsrc"

go run github.com/tc-hib/go-winres@v0.3.3 make \
	--in assets/gui/windows-gui-resources.json \
	--arch amd64,arm64 \
	--out "$resource_dir/gui"
