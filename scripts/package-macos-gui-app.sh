#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

set -euo pipefail

# Wrap the already-built GUI binary in a standard Finder application bundle.
binary_path=$1
bundle_path="${binary_path%/*}/SSH KeySelect.app"
contents_path="$bundle_path/Contents"

rm -rf "$bundle_path"
mkdir -p "$contents_path/MacOS" "$contents_path/Resources"
cp "$binary_path" "$contents_path/MacOS/ssh-keyselect-gui"
cp assets/gui/generated/platform/ssh-keyselect.icns "$contents_path/Resources/ssh-keyselect.icns"
cat > "$contents_path/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleDevelopmentRegion</key><string>en</string>
	<key>CFBundleExecutable</key><string>ssh-keyselect-gui</string>
	<key>CFBundleIconFile</key><string>ssh-keyselect</string>
	<key>CFBundleIdentifier</key><string>jp.integ.ssh-keyselect-gui</string>
	<key>CFBundleDisplayName</key><string>SSH KeySelect</string>
	<key>CFBundleName</key><string>SSH KeySelect</string>
	<key>CFBundlePackageType</key><string>APPL</string>
	<key>CFBundleShortVersionString</key><string>${APP_VERSION}</string>
	<key>LSUIElement</key><true/>
</dict>
</plist>
PLIST
printf 'APPL????' > "$contents_path/PkgInfo"
chmod 0755 "$contents_path/MacOS/ssh-keyselect-gui"
