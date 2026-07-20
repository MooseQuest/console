# syntax=docker/dockerfile:1
#
# Console — feature flags + status monitoring in one static Go binary.
# Multi-stage: cross-compile a static, cgo-free binary, then ship it on a
# distroless base. The result is a ~20 MB image that starts in milliseconds.

# ---- build stage: cross-compile on the native builder for speed ----
FROM --platform=$BUILDPLATFORM golang:1.25-bookworm AS build
ARG TARGETOS
ARG TARGETARCH
# VERSION is stamped into the binary (see cmd/console resolveVersion()).
ARG VERSION=dev
WORKDIR /src

# Download modules first so this layer caches independently of source changes.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" \
    -o /out/console ./cmd/console

# Create the default state directory here so it can be owned by nonroot in the
# final (shell-less) image.
RUN mkdir -p /data

# ---- final stage: distroless static, nonroot, CA certs for outbound TLS ----
FROM gcr.io/distroless/static-debian12:nonroot

LABEL org.opencontainers.image.title="Console" \
      org.opencontainers.image.description="Feature flags + status monitoring in one static Go binary" \
      org.opencontainers.image.licenses="AGPL-3.0-only" \
      org.opencontainers.image.source="https://github.com/MooseQuest/console"

COPY --from=build /out/console /console
COPY --from=build --chown=65532:65532 /data /data

# In a container the port must bind all interfaces to be reachable via -p; the
# container's network namespace is the trust boundary. Console still ships no
# auth, so do not publish this port to an untrusted network without a proxy.
# State lives under /data (declared a volume for persistence).
ENV CONSOLE_ADDR=0.0.0.0:8080 \
    CONSOLE_DB=/data/console.db

WORKDIR /data
EXPOSE 8080
VOLUME ["/data"]
USER nonroot:nonroot

ENTRYPOINT ["/console"]
CMD ["serve"]
