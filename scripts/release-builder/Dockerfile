# Build environment for the release binaries (issue #126).
#
# Everything that ends up in hwinfo.exe and hwinfo-bridge.exe besides the
# repository itself is fixed here: the base image by digest, the Ubuntu
# archive by snapshot date, the mingw-w64 packages by version and the Go
# distribution by checksum. The same commit therefore gives the same bytes on
# any machine, and scripts/verify-release.sh can rebuild a published release.
#
# To update: move SNAPSHOT (and the base digest) forward, set the package
# versions that snapshot carries, and release; the next release that rebuilds
# the binaries ships them. scripts/check-builder-updates.sh reports when
# Ubuntu has newer mingw-w64 packages than the ones pinned here.
FROM ubuntu:24.04@sha256:9c7f200c07e3d4a03e7249d2c16bf66a51120fb876aa2242cf31451412543289

ARG SNAPSHOT=20261120T000000Z
ARG GCC_MINGW_VERSION=13.2.0-6ubuntu1+26.1
ARG BINUTILS_MINGW_VERSION=2.41.90.20240122-1ubuntu1+11.4
ARG MINGW_W64_DEV_VERSION=11.0.1-3build1
ARG GO_VERSION=1.26.11
ARG GO_SHA256=9849f4e04ee35303714e34fbc3ac8ab44ea3e448a95aa182f91e3b9b79c710ec

# The base image has no CA certificates yet, so apt cannot verify the TLS
# certificate of the snapshot server. That does not weaken the install: apt
# still verifies every index and package against the Ubuntu archive key that
# ships in the base image.
RUN set -eux; \
    rm -f /etc/apt/sources.list.d/ubuntu.sources; \
    for suite in noble noble-updates noble-security; do \
      echo "deb https://snapshot.ubuntu.com/ubuntu/${SNAPSHOT} ${suite} main universe"; \
    done > /etc/apt/sources.list; \
    printf 'Acquire::Check-Valid-Until "false";\nAcquire::https::Verify-Peer "false";\nAcquire::https::Verify-Host "false";\n' \
      > /etc/apt/apt.conf.d/99snapshot; \
    apt-get update; \
    DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
      ca-certificates curl make \
      "gcc-mingw-w64-x86-64=${GCC_MINGW_VERSION}" \
      "binutils-mingw-w64-x86-64=${BINUTILS_MINGW_VERSION}" \
      "mingw-w64-x86-64-dev=${MINGW_W64_DEV_VERSION}"; \
    rm -rf /var/lib/apt/lists/*

RUN set -eux; \
    curl -fsSL -o /tmp/go.tgz "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz"; \
    echo "${GO_SHA256}  /tmp/go.tgz" | sha256sum -c -; \
    tar -C /usr/local -xzf /tmp/go.tgz; \
    rm /tmp/go.tgz

ENV PATH=/usr/local/go/bin:$PATH \
    HOME=/tmp/home \
    GOCACHE=/tmp/home/gocache \
    GOMODCACHE=/tmp/home/gomod \
    GOFLAGS=-mod=readonly
