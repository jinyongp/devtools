FROM golang:1.27.1-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/
COPY scripts/package.sh scripts/package.sh
COPY scripts/embed.go scripts/install.sh scripts/
RUN test -z "$(gofmt -l cmd internal scripts)" && go vet ./... && go test -race ./...
RUN set -eu; for target_os in darwin linux; do \
      for target_arch in amd64 arm64; do \
        TARGET_OS="$target_os" TARGET_ARCH="$target_arch" VERSION=0.0.0-test.1 OUTPUT_DIR=/releases sh scripts/package.sh; \
      done; \
    done \
 && VERSION=0.0.0-test.2 OUTPUT_DIR=/releases sh scripts/package.sh

FROM debian:bookworm-slim AS sandbox
RUN apt-get update \
 && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends ca-certificates curl git python3 openssl \
 && rm -rf /var/lib/apt/lists/* \
 && useradd --create-home --uid 10001 tester
COPY --from=ghcr.io/casey/just:1.58.0 /just /usr/local/bin/just
COPY --from=build /releases/ /opt/releases/
COPY scripts/install.sh /opt/devtools-install.sh
COPY docker/run.py /opt/verify/run.py
COPY docker/scenarios/ /opt/verify/scenarios/
COPY README.md /opt/verify/docs/README.md
COPY docs/install.md /opt/verify/docs/install.md
USER tester
WORKDIR /home/tester
ENV HOME=/home/tester
ENTRYPOINT ["python3", "/opt/verify/run.py"]
CMD ["all"]
