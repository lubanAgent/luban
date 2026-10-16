#!/bin/sh
set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$repo_root/sdk"

unformatted=$(gofmt -l $(find . -name '*.go' -type f))
if [ -n "$unformatted" ]; then
	printf '%s\n' "Unformatted Go files:" "$unformatted" >&2
	exit 1
fi

go mod tidy -diff
go test ./...
python3 "$repo_root/scripts/check-examples.py"
go test -race ./...
go vet ./...
python3 "$repo_root/scripts/check-sdk-deps.py"
