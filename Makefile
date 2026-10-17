# Default values for environment variables
# linux, windows, darwin
PLATFORM ?=
# amd64, arm64, arm, mips, mips64
ARCH ?=
# Docker image to use for building (docker.elastic.co/beats-dev/golang-crossbuild:1.22.4-mips-debian11)
IMAGE =

BIN_NAME = lantern-headless-$(ARCH)-$(PLATFORM)

REVISION_DATE := $(shell git log -1 --pretty=format:%ad --date=format:%Y%m%d.%H%M%S)
BUILD_DATE := $(shell date -u +%Y%m%d.%H%M%S)
UID := $(shell id -u)
GID := $(shell id -g)
LATEST_TAG = $(shell git describe --tags $(shell git rev-list --tags --max-count=1))
VERSION ?= $(subst v,,$(LATEST_TAG))

LDFLAGS := -s -w -X github.com/oldblocker/lantern-headless-iran/shared.RevisionDate=$(REVISION_DATE) -X github.com/oldblocker/lantern-headless-iran/shared.BuildDate=$(BUILD_DATE) -X github.com/oldblocker/lantern-headless-iran/shared.ApplicationVersion=$(VERSION)

# The radiance engine (full parity with official Lantern v10) is a separate
# module. It needs the "novpn" build tag (SOCKS/HTTP proxy instead of a TUN
# device) plus sing-box's protocol tags, and MUST be built with Go 1.26.x:
# sing-box reaches into golang.org/x/net/http2 via //go:linkname, and Go 1.27
# elides the target symbol, producing an "undefined reference to
# ...http2.(*Transport).connPool" link error.
#
# CGO: the Linux build needs CGO_ENABLED=1 (outline-sdk resolves CNAMEs through
# libc getaddrinfo). The Windows build uses outline-sdk's pure-Go path, so it
# cross-compiles with CGO_ENABLED=0:
#   CGO_ENABLED=0 GOOS=windows GOARCH=amd64 make radiance-engine
RADIANCE_TAGS ?= novpn,with_gvisor,with_quic,with_dhcp,with_wireguard,with_utls,with_acme
RADIANCE_TOOLCHAIN ?= go1.26.9

radiance-engine:
	@mkdir -p bin
	cd radiance-engine && GOTOOLCHAIN=$(RADIANCE_TOOLCHAIN) go build -tags "$(RADIANCE_TAGS)" -o ../bin/lantern-radiance .
	@echo "Built bin/lantern-radiance"

# One-shot local builds of the CLI + radiance engine for a single platform.
# Output goes to dist/ (see scripts/build-linux.sh and scripts/build-windows.sh).
build-local-linux:
	./scripts/build-linux.sh

build-local-windows:
	./scripts/build-windows.sh

# Check if required environment variables are set
check-env:
	@mkdir -p build
	@if [ -z "$(PLATFORM)" ]; then \
		echo "Error: PLATFORM is not set"; \
		exit 1; \
	fi
	@if [ -z "$(IMAGE)" ]; then \
		echo "Error: IMAGE is not set"; \
		exit 1; \
	fi
	@if [ -z "$(ARCH)" ]; then \
		echo "Error: ARCH is not set"; \
		exit 1; \
	fi


# Actually Build the application (called inside the build container)
build-internal: export GOPRIVATE = github.com/getlantern
build-internal: export CGO_ENABLED=1
build-internal: check-env
	@apt update
	@apt install git-lfs
	@git lfs install
	go build -ldflags="$(LDFLAGS)" -buildvcs=false -o ./build/$(BIN_NAME) .
	chown $(TARGET_UID):$(TARGET_GID) ./build/$(BIN_NAME)

clean:
	@echo "Cleaning up"
	rm -f build/*

# Build the application using build container
build: check-env
	@echo "Building for $(PLATFORM)/$(ARCH) version $(VERSION) using $(IMAGE)"
	docker run -v .:/src -w /src -e TARGET_UID=$(UID) -e TARGET_GID=$(GID) \
        $(IMAGE) \
        --build-cmd "make VERSION=$(VERSION) PLATFORM=$(PLATFORM) ARCH=$(ARCH) IMAGE=$(IMAGE) build-internal" \
        -p $(PLATFORM)/$(ARCH)

proto:
	go install github.com/bufbuild/buf/cmd/buf@latest
	cd ./auth && buf generate



# Show help
help:
	@echo "Available targets:"
	@echo "  build    - Build the application (requires PLATFORM, ARCH, IMAGE and VERSION)"
	@echo "  radiance-engine - Build the radiance engine helper (bin/lantern-radiance) natively"
	@echo "  build-local-linux   - Build dist/ for Linux (CLI + radiance helper)"
	@echo "  build-local-windows - Cross-build dist/ for Windows (CLI + radiance helper)"
	@echo "  clean    - Remove build/ contents"
	@echo "  help     - Show this help message"
	@echo ""
	@echo "Required environment variables:"
	@echo "  PLATFORM - Target platform"
	@echo "  ARCH     - Target architecture"
	@echo "  VERSION  - Build version"

.PHONY: build clean help check-env package radiance-engine build-local-linux build-local-windows

# Default target
.DEFAULT_GOAL := help
