#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

set -euo pipefail

target="${1:-}"
version="${2:-}"
version="${version#v}"

if [[ "$target" != macos ]]; then
	echo "usage: $0 macos vX.Y.Z" >&2
	exit 2
fi

if [[ -z "$version" ]]; then
	echo "a release version is required" >&2
	exit 2
fi

# MyGo reads its app version from mygo.json, so use the tag version for this build only.
config_backup="$(mktemp)"
cp mygo.json "$config_backup"
restore_config() {
	cp "$config_backup" mygo.json
	rm -f "$config_backup"
}
trap restore_config EXIT

python3 - "$version" <<'PY'
import json
import sys
from pathlib import Path

path = Path("mygo.json")
config = json.loads(path.read_text(encoding="utf-8"))
config["version"] = sys.argv[1]
path.write_text(json.dumps(config, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
PY

# Keep the DMG metadata aligned with the release or snapshot version.
if [[ ! -s CREDITS || ! -s internal/credits/dependencies.txt ]]; then
	bash scripts/generate-credits.sh
fi

build_flags="-tags=gui"
if [[ -n "${GOFLAGS:-}" ]]; then
	build_flags="$GOFLAGS $build_flags"
fi

# Replace MyGo's PNG-based app and volume icons with this project's Finder-compatible ICNS.
patch_dmg_icons() (
	set -euo pipefail
	local dmg_path="$1"
	local work_dir writable_dmg patched_dmg mount_point device
	work_dir="$(mktemp -d)"
	writable_dmg="$work_dir/writable.dmg"
	patched_dmg="$work_dir/patched.dmg"
	mount_point="$work_dir/mount"
	device=""
	trap 'if [[ -n "$device" ]]; then hdiutil detach -force "$device" >/dev/null 2>&1 || true; fi; rm -rf "$work_dir"' EXIT

	hdiutil convert "$dmg_path" -format UDRW -ov -o "$writable_dmg"
	mkdir -p "$mount_point"
	device="$(hdiutil attach -readwrite -noverify -noautoopen -nobrowse -mountpoint "$mount_point" "$writable_dmg" | awk '$1 ~ /^\/dev\/disk/ && device == "" { device = $1 } END { print device }')"
	if [[ -z "$device" ]]; then
		echo "could not find the attached DMG device" >&2
		return 1
	fi

	cp assets/gui/generated/platform/ssh-keyselect.icns "$mount_point/SSH KeySelect.app/Contents/Resources/AppIcon.icns"
	cp assets/gui/generated/platform/ssh-keyselect.icns "$mount_point/.VolumeIcon.icns"
	# Finder's custom-icon flag is bit 0x0400 at byte offset 8 in FinderInfo.
	xattr -wx com.apple.FinderInfo 0000000000000000040000000000000000000000000000000000000000000000 "$mount_point"
	hdiutil detach "$device"
	device=""
	hdiutil convert "$writable_dmg" -format ULMO -ov -o "$patched_dmg"
	mv -f "$patched_dmg" "$dmg_path"
)

build_macos() {
	rm -rf dist/mygo release-packages/macos
	go run ./scripts/generate-platform-icons.go
	GOFLAGS="$build_flags" go tool mygo build -platform darwin/universal
	local app_path="dist/mygo/darwin-universal/SSH KeySelect.app"
	local dmg_path="dist/mygo/darwin-universal/SSH KeySelect $version.dmg"
	# Also fix MyGo's standalone app bundle; its DMG contains a separate copy patched below.
	cp assets/gui/generated/platform/ssh-keyselect.icns "$app_path/Contents/Resources/AppIcon.icns"
	patch_dmg_icons "$dmg_path"
	mkdir -p release-packages/macos
	cp "$dmg_path" release-packages/macos/
}

build_macos
