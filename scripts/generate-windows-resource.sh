#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

action="${1:-}"
case "$action" in
prepare|clean)
	;;
*)
	echo "usage: $0 {prepare|clean} {ssh-keyselect|ssh-keyselect-gui}" >&2
	exit 2
	;;
esac

command_name="${2:-}"
case "$command_name" in
ssh-keyselect)
	resource_config="assets/gui/windows-cli-resources.json"
	resource_prefix="rsrc"
	;;
ssh-keyselect-gui)
	resource_config="assets/gui/windows-gui-resources.json"
	resource_prefix="gui"
	;;
*)
	echo "usage: $0 {prepare|clean} {ssh-keyselect|ssh-keyselect-gui}" >&2
	exit 2
	;;
esac

if [[ "${SSH_KEYSELECT_TARGET_OS:-}" != windows ]]; then
	exit 0
fi

target_arch="${SSH_KEYSELECT_TARGET_ARCH:?GoReleaser target architecture is required}"
resource_output="cmd/$command_name/$resource_prefix"
resource_path="${resource_output}_windows_${target_arch}.syso"

case "$action" in
prepare)
	version="${SSH_KEYSELECT_VERSION:?GoReleaser version is required}"
	host_os="$(go env GOHOSTOS)"
	host_arch="$(go env GOHOSTARCH)"
	# Refresh this target's resource so both executables show the release version in Windows.
	env GOOS="$host_os" GOARCH="$host_arch" go run github.com/tc-hib/go-winres@v0.3.3 make \
		--in "$resource_config" \
		--arch "$target_arch" \
		--out "$resource_output" \
		--file-version "$version" \
		--product-version "$version"
	;;
clean)
	rm -f "$resource_path"
	;;
esac
