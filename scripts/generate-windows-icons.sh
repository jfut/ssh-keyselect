#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

icon_path="assets/gui/generated/ssh-keyselect-icon.ico"
resource_dir="assets/gui/generated/windows"
mkdir -p "$resource_dir"

for architecture in amd64 arm64; do
	go run github.com/akavel/rsrc@v0.10.2 \
		-arch "$architecture" \
		-ico "$icon_path" \
		-o "$resource_dir/rsrc_windows_${architecture}.syso"
done

# Keep the GUI icon and friendly name in one resource object for the Go linker.
go run github.com/tc-hib/go-winres@v0.3.3 make \
	--in assets/gui/windows-gui-resources.json \
	--arch amd64,arm64 \
	--out "$resource_dir/gui"
