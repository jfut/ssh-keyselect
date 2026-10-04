#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

set -euo pipefail

action="${1:-}"
case "$action" in
prepare|clean)
	;;
*)
	echo "usage: $0 {prepare|clean}" >&2
	exit 2
	;;
esac

if [[ "${SSH_KEYSELECT_TARGET_OS:-}" != windows ]]; then
	exit 0
fi

target_arch="${SSH_KEYSELECT_TARGET_ARCH:?GoReleaser target architecture is required}"
resource_path="cmd/ssh-keyselect-gui/gui_windows_${target_arch}.syso"

case "$action" in
prepare)
	version="${SSH_KEYSELECT_VERSION:?GoReleaser version is required}"
	host_os="$(go env GOHOSTOS)"
	host_arch="$(go env GOHOSTARCH)"
	# Refresh the GUI resource for this target so Windows file properties match the release tag.
	env GOOS="$host_os" GOARCH="$host_arch" go run github.com/tc-hib/go-winres@v0.3.3 make \
		--in assets/gui/windows-gui-resources.json \
		--arch "$target_arch" \
		--out cmd/ssh-keyselect-gui/gui \
		--file-version "$version" \
		--product-version "$version"
	;;
clean)
	rm -f "$resource_path"
	;;
esac
