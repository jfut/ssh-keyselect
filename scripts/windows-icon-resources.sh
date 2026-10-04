#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

resource_dir="assets/gui/generated/windows"
command_dirs=(cmd/ssh-keyselect)
gui_command_dir="cmd/ssh-keyselect-gui"
architectures=(amd64 arm64)

# Go only picks up a .syso file from the command package directory, so stage
# ignored copies from assets only for the duration of a build.
clean_staged_resources() {
	for command_dir in "${command_dirs[@]}"; do
		for architecture in "${architectures[@]}"; do
			rm -f "$command_dir/rsrc_windows_${architecture}.syso"
		done
	done
	for architecture in "${architectures[@]}"; do
		rm -f "$gui_command_dir/rsrc_windows_${architecture}.syso"
		rm -f "$gui_command_dir/gui_windows_${architecture}.syso"
	done
}

stage_resources() {
	clean_staged_resources
	for architecture in "${architectures[@]}"; do
		source="$resource_dir/rsrc_windows_${architecture}.syso"
		if [[ ! -s "$source" ]]; then
			echo "missing generated Windows icon resource: $source" >&2
			return 1
		fi
		for command_dir in "${command_dirs[@]}"; do
			cp "$source" "$command_dir/rsrc_windows_${architecture}.syso"
		done

		source="$resource_dir/gui_windows_${architecture}.syso"
		if [[ ! -s "$source" ]]; then
			echo "missing generated Windows GUI resource: $source" >&2
			return 1
		fi
		cp "$source" "$gui_command_dir/gui_windows_${architecture}.syso"
	done
}

usage() {
	echo "usage: $0 {stage|clean|with} [command ...]" >&2
	exit 2
}

case "${1:-}" in
stage)
	trap clean_staged_resources EXIT
	stage_resources
	trap - EXIT
	;;
clean)
	clean_staged_resources
	;;
with)
	shift
	[[ $# -gt 0 ]] || usage
	trap clean_staged_resources EXIT
	trap 'exit 130' INT
	trap 'exit 143' TERM
	stage_resources
	"$@"
	;;
*)
	usage
	;;
esac
