#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0
# SPDX-FileCopyrightText: Copyright contributors to the ssh-keyselect project.

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

credits_workdir="$(mktemp -d)"
trap 'rm -rf "$credits_workdir"' EXIT
mkdir -p "$credits_workdir/bin"

# Scan each released operating system with the GUI build tag so platform-specific MyGo dependencies are included.
GOBIN="$credits_workdir/bin" go install github.com/google/go-licenses@v1.6.0
go_licenses="$credits_workdir/bin/go-licenses"

for target_os in linux darwin windows; do
	GOOS="$target_os" GOFLAGS=-tags=gui "$go_licenses" csv ./... > "$credits_workdir/$target_os.csv"
	GOOS="$target_os" GOFLAGS=-tags=gui "$go_licenses" save ./... --save_path="$credits_workdir/save-$target_os" --force
done

root_module="$(go list -m)"
GOFLAGS=-tags=gui go list -m -f '{{printf "%s\t%s" .Path .Version}}' all > "$credits_workdir/modules.tsv"
cat "$credits_workdir/linux.csv" "$credits_workdir/darwin.csv" "$credits_workdir/windows.csv" |
	sort -u > "$credits_workdir/licenses.csv"

# Collapse platform-specific package imports to one entry per Go module.
awk -v root="$root_module" '
	FNR == NR {
		if ($1 != root) versions[$1] = $2
		next
	}
	{
		count = split($0, fields, ",")
		if (count < 3) next
		package = fields[1]
		url = fields[2]
		license = fields[3]
		best = ""
		for (module in versions) {
			if ((package == module || index(package, module "/") == 1) && length(module) > length(best)) best = module
		}
		if (best != "") {
			licenses[best] = license
			urls[best] = url
		}
	}
	END {
		for (module in licenses) {
			if (versions[module] == "") {
				printf "No module version for license result %s\n", module > "/dev/stderr"
				exit 1
			}
			printf "%s\t%s\t%s\t%s\n", module, versions[module], licenses[module], urls[module]
		}
	}
' "$credits_workdir/modules.tsv" "$credits_workdir/licenses.csv" |
	sort -t $'\t' -k1,1 > "$credits_workdir/dependencies.tsv"

{
	printf 'Go standard library: BSD-3-Clause\n'
	while IFS=$'\t' read -r module version license _; do
		printf '%s %s: %s\n' "$module" "$version" "$license"
	done < "$credits_workdir/dependencies.tsv"
} > internal/credits/dependencies.txt

{
	printf 'ssh-keyselect third-party license notices\n'
	printf 'The project license is in LICENSE. These notices cover third-party dependencies.\n\n'
	printf 'Go standard library: BSD-3-Clause\nhttps://go.dev/LICENSE\n----------------------------------------------------------------\n'
	cat "$(go env GOROOT)/LICENSE"
	while IFS=$'\t' read -r module version license url; do
		printf '\n\n%s %s: %s\n%s\n----------------------------------------------------------------\n' "$module" "$version" "$license" "$url"
		license_file=""
		for target_os in linux darwin windows; do
			module_dir="$credits_workdir/save-$target_os/$module"
			if [[ -d "$module_dir" ]]; then
				license_file="$(find "$module_dir" -type f \( -iname 'LICENSE' -o -iname 'LICENSE.*' -o -iname 'COPYING' -o -iname 'COPYING.*' \) -print | sort | head -n 1)"
				if [[ -n "$license_file" ]]; then break; fi
			fi
		done
		if [[ -z "$license_file" ]]; then
			printf 'Could not find license text for %s\n' "$module" >&2
			exit 1
		fi
		cat "$license_file"
	done < "$credits_workdir/dependencies.tsv"
} > CREDITS
