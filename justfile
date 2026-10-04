set dotenv-load := true
set export := true
set positional-arguments := true

NAME := "ssh-keyselect"
GO_EXE_SUFFIX := `go env GOEXE`

default:
    @just --list

#
# clean
#

clean:
	rm -rf dist CREDITS assets/gui/generated
	bash scripts/windows-icon-resources.sh clean
	mkdir -p dist

#
# update
#

update: update-aqua update-go

update-aqua:
    aqua update
    aqua update-checksum --prune
    aqua i -l

update-go:
    go get -t -u ./...
    go mod tidy

#
# deps
#

deps:
    go mod download

deps-credits:
    bash scripts/generate-credits.sh

deps-credits-if-missing:
    if [ ! -s CREDITS ] || [ ! -s internal/credits/dependencies.txt ]; then bash scripts/generate-credits.sh; fi

#
# dev
#

fmt:
    gofmt -w .

lint:
    golangci-lint run ./...

test: deps-credits-if-missing
    go test ./...
    go test -tags gui ./...

help *ARGS:
	go run ./cmd/ssh-keyselect {{ARGS}} --help

#
# gen
#

gen-platform-icons:
	go run ./scripts/generate-platform-icons.go

gen-windows-icons: gen-platform-icons
	bash scripts/generate-windows-icons.sh

#
# build
#

build: clean deps gen-windows-icons deps-credits-if-missing
	bash scripts/windows-icon-resources.sh with env CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o dist/ssh-keyselect{{GO_EXE_SUFFIX}} ./cmd/ssh-keyselect
	if [ "$(go env GOOS)" = "windows" ]; then \
	  bash scripts/windows-icon-resources.sh with env CGO_ENABLED=0 go build -trimpath -tags gui -ldflags "-s -w -H windowsgui" -o dist/ssh-keyselect-gui{{GO_EXE_SUFFIX}} ./cmd/ssh-keyselect-gui; \
	else \
	  bash scripts/windows-icon-resources.sh with env CGO_ENABLED=0 go build -trimpath -tags gui -ldflags "-s -w" -o dist/ssh-keyselect-gui{{GO_EXE_SUFFIX}} ./cmd/ssh-keyselect-gui; \
	fi

#
# run
#

run *ARGS: build
    # Pass "$@" as-is via positional-arguments to avoid misparsing queries that include `>`.
    ./dist/ssh-keyselect{{GO_EXE_SUFFIX}} "$@"

#
# release
#

snapshot: deps gen-windows-icons deps-credits-if-missing
    bash scripts/windows-icon-resources.sh with goreleaser release --skip=publish --clean --snapshot

release: deps gen-windows-icons deps-credits
    bash scripts/windows-icon-resources.sh with goreleaser release --skip=publish --clean --skip=validate
