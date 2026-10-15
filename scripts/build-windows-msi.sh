#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

version="${1:-}"
version="${version#v}"
if [[ -z "$version" ]]; then
	echo "usage: $0 vX.Y.Z[-prerelease]" >&2
	exit 2
fi

# Generate the shared app icon and license inventory when the script is run directly.
if [[ ! -s assets/gui/generated/ssh-keyselect-icon.ico ]]; then
	go run ./scripts/generate-platform-icons.go
fi
if [[ ! -s assets/gui/generated/windows/gui_windows_amd64.syso || ! -s assets/gui/generated/windows/gui_windows_arm64.syso ]]; then
	bash scripts/generate-windows-icons.sh
fi
if [[ ! -s CREDITS || ! -s internal/credits/dependencies.txt ]]; then
	bash scripts/generate-credits.sh
fi

commit="$(git rev-parse HEAD)"
rm -rf dist/windows release-packages/windows
mkdir -p dist/windows release-packages/windows

cleanup_resources() {
	for architecture in amd64 arm64; do
		for command_name in ssh-keyselect ssh-keyselect-gui; do
			SSH_KEYSELECT_TARGET_OS=windows \
			SSH_KEYSELECT_TARGET_ARCH="$architecture" \
			SSH_KEYSELECT_VERSION="$version" \
				bash scripts/generate-windows-resource.sh clean "$command_name" || true
		done
	done
}
trap cleanup_resources EXIT

for architecture in amd64 arm64; do
	SSH_KEYSELECT_TARGET_OS=windows \
	SSH_KEYSELECT_TARGET_ARCH="$architecture" \
	SSH_KEYSELECT_VERSION="$version" \
		bash scripts/generate-windows-resource.sh prepare ssh-keyselect

	cli_executable="dist/windows/$architecture/ssh-keyselect.exe"
	env GOOS=windows GOARCH="$architecture" CGO_ENABLED=0 \
		go build -trimpath \
		-ldflags "-s -w -X main.version=$version -X main.commit=$commit" \
		-o "$cli_executable" ./cmd/ssh-keyselect

	SSH_KEYSELECT_TARGET_OS=windows \
	SSH_KEYSELECT_TARGET_ARCH="$architecture" \
	SSH_KEYSELECT_VERSION="$version" \
		bash scripts/generate-windows-resource.sh clean ssh-keyselect

	SSH_KEYSELECT_TARGET_OS=windows \
	SSH_KEYSELECT_TARGET_ARCH="$architecture" \
	SSH_KEYSELECT_VERSION="$version" \
		bash scripts/generate-windows-resource.sh prepare ssh-keyselect-gui

	executable="dist/windows/$architecture/ssh-keyselect-gui.exe"
	env GOOS=windows GOARCH="$architecture" CGO_ENABLED=0 \
		go build -trimpath -tags gui \
		-ldflags "-s -w -H windowsgui -X main.version=$version -X main.commit=$commit" \
		-o "$executable" ./cmd/ssh-keyselect-gui

	SSH_KEYSELECT_TARGET_OS=windows \
	SSH_KEYSELECT_TARGET_ARCH="$architecture" \
	SSH_KEYSELECT_VERSION="$version" \
		bash scripts/generate-windows-resource.sh clean ssh-keyselect-gui

	go run ./scripts/build-windows-msi \
		"$version" "$architecture" "$commit" "$cli_executable" "$executable" \
		"release-packages/windows/SSH-KeySelect-Setup-$version-$architecture.msi"
done
