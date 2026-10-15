#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

set -euo pipefail

# Match the release tag for local release builds and GoReleaser's snapshot template.
mode="${1:-}"
case "$mode" in
snapshot|release)
	;;
*)
	echo "usage: $0 {snapshot|release}" >&2
	exit 2
	;;
esac

tag="$(git describe --tags --abbrev=0)"
version="${tag#v}"

if [[ "$mode" == release ]]; then
	printf '%s\n' "$version"
	exit 0
fi

python3 - "$version" <<'PY'
import re
import sys

match = re.fullmatch(r"(\d+)\.(\d+)\.(\d+)(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?", sys.argv[1])
if match is None:
    raise SystemExit(f"cannot derive snapshot version from git tag: {sys.argv[1]}")

major, minor, patch = (int(part) for part in match.groups())
print(f"{major}.{minor}.{patch + 1}-next")
PY
