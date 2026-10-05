#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

resource_dir="assets/gui/generated/windows"
resource_targets=(cmd/ssh-keyselect/rsrc cmd/ssh-keyselect-gui/gui)
architectures=(amd64 arm64)

# Go only picks up a .syso file from the command package directory, so stage
# ignored copies from assets only for the duration of a build.
clean_staged_resources() {
	for resource_target in "${resource_targets[@]}"; do
		for architecture in "${architectures[@]}"; do
			rm -f "${resource_target}_windows_${architecture}.syso"
		done
	done
}

stage_resources() {
	clean_staged_resources
	for resource_target in "${resource_targets[@]}"; do
		for architecture in "${architectures[@]}"; do
			source="$resource_dir/${resource_target##*/}_windows_${architecture}.syso"
			if [[ ! -s "$source" ]]; then
				echo "missing generated Windows resource: $source" >&2
				return 1
			fi
			cp "$source" "${resource_target}_windows_${architecture}.syso"
		done
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
