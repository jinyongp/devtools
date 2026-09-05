set positional-arguments

export VERIFY_IMAGE := env('VERIFY_IMAGE', 'devtools-sandbox:local')

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

verify-docker +scenarios='all':
    docker build -f docker/Dockerfile -t "$VERIFY_IMAGE" .
    docker run --rm --network none --cap-drop ALL --security-opt no-new-privileges "$VERIFY_IMAGE" "$@"
