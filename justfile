set positional-arguments

default:
    @just --list

build:
    go build -o bin/devtools ./cmd/devtools

fmt:
    gofmt -w cmd internal

check:
    test -z "$(gofmt -l cmd internal)"
    go vet ./...
    go test -race ./...

test:
    go test ./...

vet:
    go vet ./...

clean:
    rm -rf bin coverage.out

release version:
    VERSION="$1" sh scripts/package.sh
