set positional-arguments

export VERIFY_IMAGE := env('VERIFY_IMAGE', 'devtools-sandbox:local')

default:
    @just --list

build:
    go build -o bin/devtools ./cmd/devtools

# Serve dashboard source files; refresh the browser after UI edits. Ctrl+C stops it.
dashboard:
    go build -o bin/dashboard ./scripts/dashboard
    ./bin/dashboard

fmt:
    gofmt -w cmd internal scripts skills

check: check-skill-release check-dashboard check-installer
    test -z "$(gofmt -l cmd internal scripts skills)"
    go vet ./...
    go test -race ./...

check-dashboard:
    node --test scripts/dashboard-*.test.mjs

check-installer:
    sh -n scripts/install.sh
    sh scripts/check-installer.sh
    DEVTOOLS_TEST_README=README.md DEVTOOLS_TEST_INSTALL_DOC=docs/install.md python3 docker/scenarios/bootstrap.py

check-skill:
    sh scripts/validate-skill.sh

check-skill-release:
    sh scripts/check-skill-release.sh

test:
    go test ./...

vet:
    go vet ./...

clean:
    rm -rf bin coverage.out

release version:
    VERSION="$1" sh scripts/package.sh

release-skill version:
    VERSION="$1" sh scripts/package-skill.sh

verify-docker +scenarios='all':
    docker build -f docker/Dockerfile -t "$VERIFY_IMAGE" .
    docker run --rm --network none --cap-drop ALL --security-opt no-new-privileges "$VERIFY_IMAGE" "$@"
