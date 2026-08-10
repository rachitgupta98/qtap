# syntax=docker/dockerfile:1

# =============================================================================
# Qtap container image
#
# Builds the eBPF traffic capture agent for Linux (amd64/arm64).
# Intended for Jenkins artifact-registry pipelines and Kubernetes DaemonSet
# deployment (privileged, hostPID, hostNetwork).
#
# Example (local):
#   docker build -t qtap:local .
#
# Example (Jenkins / artifact registry):
#   docker build \
#     --build-arg VERSION="${GIT_TAG}" \
#     --build-arg GIT_COMMIT="${GIT_COMMIT}" \
#     --build-arg GIT_BRANCH="${GIT_BRANCH}" \
#     -t "${ARTIFACT_REGISTRY}/qtap:${IMAGE_TAG}" \
#     .
#   docker push "${ARTIFACT_REGISTRY}/qtap:${IMAGE_TAG}"
# =============================================================================

ARG GO_VERSION=1.26.5

# -----------------------------------------------------------------------------
# Build stage: compile qtap (Go + eBPF via bpf2go/clang)
# -----------------------------------------------------------------------------
FROM golang:${GO_VERSION}-bookworm AS builder

ARG TARGETARCH
ARG VERSION=dev
ARG GIT_COMMIT=unknown
ARG GIT_BRANCH=unknown

ENV CGO_ENABLED=0 \
    GOOS=linux \
    GOARCH=${TARGETARCH}

# clang-14 is required for eBPF object generation (see README / CI workflow).
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    clang-14 \
    llvm-14 \
    libelf-dev \
    linux-libc-dev \
    make \
    git \
    && rm -rf /var/lib/apt/lists/* \
    && update-alternatives --install /usr/bin/clang clang /usr/bin/clang-14 100 \
    && update-alternatives --install /usr/bin/llvm-strip llvm-strip /usr/bin/llvm-strip-14 100

WORKDIR /src

# Cache Go module downloads separately from source changes.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Inject build metadata consumed by pkg/buildinfo via Makefile LD_FLAGS.
ENV GIT_VERSION=${VERSION} \
    GIT_COMMIT=${GIT_COMMIT} \
    GIT_BRANCH=${GIT_BRANCH}

RUN make generate build

# -----------------------------------------------------------------------------
# Runtime stage: minimal Linux image with tini init
# -----------------------------------------------------------------------------
FROM debian:bookworm-slim AS runtime

ARG VERSION=dev
ARG GIT_COMMIT=unknown

LABEL org.opencontainers.image.title="qtap" \
      org.opencontainers.image.description="Qtap eBPF traffic capture agent" \
      org.opencontainers.image.source="https://github.com/qpoint-io/qtap" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${GIT_COMMIT}"

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    tini \
    && rm -rf /var/lib/apt/lists/*

# Matches Kubernetes manifests that mount config at /app/tap-config.yaml
# and set QPOINT_CONFIG=/app/tap-config.yaml.
WORKDIR /app

COPY --from=builder /src/bin/qtap /usr/local/bin/qtap

# Default metrics/status port (override with STATUS_LISTEN in K8s).
EXPOSE 10001

ENV TINI_SUBREAPER=1

ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/qtap"]
CMD ["--log-level=info"]
