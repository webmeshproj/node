NAME  ?= node
CTL   ?= wmctl
REPO  ?= ghcr.io/webmeshproj
IMAGE ?= $(REPO)/$(NAME):latest

BUILD_IMAGE ?= $(REPO)/node-buildx:latest

VERSION_PKG := github.com/webmeshproj/$(NAME)/pkg/version
VERSION     := $(shell git describe --tags --always --dirty)
COMMIT      := $(shell git rev-parse HEAD)
DATE        := $(shell date -u '+%Y-%m-%dT%H:%M:%SZ')

ARCH  ?= $(shell go env GOARCH)
OS    ?= $(shell go env GOOS)

ifeq ($(OS),Windows_NT)
	OS := windows
endif

BUILD_TAGS  ?= osusergo,netgo
LDFLAGS     ?= -s -w \
				-X $(VERSION_PKG).Version=$(VERSION) \
				-X $(VERSION_PKG).Commit=$(COMMIT) \
				-X $(VERSION_PKG).BuildDate=$(DATE)

DIST  := $(CURDIR)/dist

default: build

##@ General

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Build

ifeq ($(OS),windows)
# Generate is buggy on windows depending on the setup, so comment out for local dev.
# The windows binary is built via Linux in CI.
build: fmt vet ## Build node binary for the local platform.
else
build: fmt vet generate
endif
	CGO_ENABLED=0 go build \
		-tags "$(BUILD_TAGS)" \
		-ldflags "$(LDFLAGS)" \
		-o "$(DIST)/$(NAME)_$(OS)_$(ARCH)" \
		cmd/$(NAME)/main.go

build-ctl: fmt vet ## Build wmctl binary for the local platform.
	CGO_ENABLED=0 go build \
		-tags "$(BUILD_TAGS)" \
		-ldflags "$(LDFLAGS)" \
		-o "$(DIST)/$(CTL)_$(OS)_$(ARCH)" \
		cmd/$(CTL)/main.go

DIST_TEMPLATE := {{.Dir}}_{{.OS}}_{{.Arch}}
DIST_PARALLEL ?= -1

LINUX_ARCHS := amd64 arm64 arm 386 s390x ppc64le mips64 mips64le mips mipsle
dist-linux: generate ## Build distribution binaries for all Linux platforms.
	go run github.com/mitchellh/gox@latest \
		-os="linux" \
		-arch="$(LINUX_ARCHS)" \
		-tags "$(BUILD_TAGS)" \
		-ldflags "$(LDFLAGS)" \
		-parallel="$(DIST_PARALLEL)" \
		-output="$(DIST)/$(DIST_TEMPLATE)" \
		./cmd/$(NAME) ./cmd/$(CTL)
	# We can compress all but the s390x/mips64 binaries.
	upx --best --lzma \
		$(DIST)/*_linux_amd64 \
		$(DIST)/*_linux_arm* \
		$(DIST)/*_linux_386 \
		$(DIST)/*_linux_ppc64le \
		$(DIST)/*_linux_mips \
		$(DIST)/*_linux_mipsle

WINDOWS_ARCHS := amd64
dist-windows: generate ## Build distribution binaries for Windows.
	go run github.com/mitchellh/gox@latest \
		-os="windows" \
		-arch="$(WINDOWS_ARCHS)" \
		-tags "$(BUILD_TAGS)" \
		-ldflags "$(LDFLAGS)" \
		-parallel="$(DIST_PARALLEL)" \
		-output="$(DIST)/$(DIST_TEMPLATE)" \
		./cmd/$(NAME) ./cmd/$(CTL)
	upx --best --lzma $(DIST)/*_windows_*

DARWIN_ARCHS := amd64 arm64
dist-darwin: generate ## Build distribution binaries for Darwin.
	go run github.com/mitchellh/gox@latest \
		-os="darwin" \
		-arch="$(DARWIN_ARCHS)" \
		-tags "$(BUILD_TAGS)" \
		-ldflags "$(LDFLAGS)" \
		-parallel="$(DIST_PARALLEL)" \
		-output="$(DIST)/$(DIST_TEMPLATE)" \
		./cmd/$(NAME) ./cmd/$(CTL)
	# Only compress the amd64 binaries. M1/M2 platforms are tricky.
	upx --best --lzma $(DIST)/*_darwin_amd64

DOCKER ?= docker

docker-build: build ## Build the node docker image
	$(DOCKER) build \
		-f Dockerfile \
		--build-arg TARGETOS=$(OS) \
		--build-arg TARGETARCH=$(ARCH) \
		-t $(IMAGE) .

docker-build-distroless: build ## Build the distroless node docker image
	$(DOCKER) build \
		-f Dockerfile.distroless \
		--build-arg TARGETOS=$(OS) \
		--build-arg TARGETARCH=$(ARCH) \
		-t $(IMAGE)-distroless .

docker-push: docker-build ## Push the node docker image
	$(DOCKER) push $(IMAGE)

docker-push-distroless: docker-build-distroless ## Push the distroless node docker image
	$(DOCKER) push $(IMAGE)-distroless

##@ Testing

COVERAGE_FILE ?= coverage.out
TEST_PARALLEL ?= 1
TEST_ARGS     ?= -v -cover -tags "$(BUILD_TAGS)" -coverprofile=$(COVERAGE_FILE) -covermode=atomic -parallel=$(TEST_PARALLEL)
test: fmt vet
	go test $(TEST_ARGS) ./...
	go tool cover -func=$(COVERAGE_FILE)

lint: ## Run linters.
	go run github.com/golangci/golangci-lint/cmd/golangci-lint@latest run

.PHONY: fmt
fmt: ## Run go fmt against code.
ifeq ($(OS),windows)
	echo "Skipping go fmt on windows"
else
	go fmt ./...
endif

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

##@ Misc

generate: ## Run go generate against code.
	go generate ./...

install-ctl: build-ctl ## Install wmctl binary into $GOPATH/bin.
	install -m 755 $(DIST)/$(CTL)_$(OS)_$(ARCH) $(shell go env GOPATH)/bin/$(CTL)

clean: ## Clean up build and development artifacts.
	rm -rf dist
