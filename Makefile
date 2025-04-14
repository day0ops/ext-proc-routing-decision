MKFILE_PATH := $(lastword $(MAKEFILE_LIST))
PROJ_DIR := $(abspath $(patsubst %/,%,$(dir $(abspath $(MKFILE_PATH)))))

# Tags specific for building
GOTAGS ?=

# Number of procs to use
GOMAXPROCS ?= 4

# Common project props
GOVERSION ?= 1.24
REPO ?= australia-southeast1-docker.pkg.dev/solo-test-236622/apac
GIT_COMMIT ?= $(shell git rev-parse --short HEAD)

# Get the main project props
NAME ?= ext-proc-routing-decision
VERSION ?= $(shell git describe --tags --always --dirty 2> /dev/null || echo v0)
LD_FLAGS ?= -s -w \
	-X 'github.com/day0ops/ext-proc-routing-decision/pkg/version.Name=${NAME}' \
	-X 'github.com/day0ops/ext-proc-routing-decision/pkg/version.Version=${VERSION}' \
	-X 'github.com/day0ops/ext-proc-routing-decision/pkg/version.GitCommit=${GIT_COMMIT}'

# Current system information
GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)

# Default os-arch combination to build
XC_OS ?= darwin linux windows
XC_ARCH ?= amd64 arm64
XC_EXCLUDE ?=

DOCKER_SUPPORTED_PLATFORMS ?= linux/amd64,linux/arm64

# Output dir for binaries
BIN_OUT_DIR ?= bin

BUILD_OPTS ?=
ifeq ($(PUSH_MULTIARCH), true)
BUILDX_ARG_PUSH = '--push'
endif

PODMAN_EXISTS := $(shell command -v podman 2> /dev/null)

## all: run all targets
all: clean tidy test build
.PHONY: all

## tidy: format the source
tidy:
	@pushd ${PROJ_DIR} >/dev/null;go fmt ./...;go mod tidy -v;popd >/dev/null
.PHONY: tidy

# ------------------------------------------------------------------------------------------------------------
# Targets for build
# ------------------------------------------------------------------------------------------------------------

# macro for building all variants
define make-xc-target
  $(BIN_OUT_DIR)/$1_$2/$(NAME)$(if $(findstring windows,$1),.exe,):
  ifneq (,$(findstring ${1}/${2},$(XC_EXCLUDE)))
		@printf "==> Building %s%20s %s\n" "-->" "${1}/${2}:" "${PROJ_DIR} (excluded)"
  else
		@printf "==> Building %s%20s %s\n" "-->" "${1}/${2}:" "${PROJ_DIR}"
		@mkdir -p "${PROJ_DIR}/${BIN_OUT_DIR}"
		@docker run \
			--interactive \
			--rm \
			--dns="8.8.8.8" \
			--security-opt label=disable \
			--volume="${PROJ_DIR}/${BIN_OUT_DIR}:/go/src/bin" \
			--volume="${PROJ_DIR}:/go/src/build" \
			--workdir="/go/src/build" \
			"golang:${GOVERSION}" \
			env \
				CGO_ENABLED="0" \
				GOOS="${1}" \
				GOARCH="${2}" \
				go build \
					-a \
					-o="/go/src/bin/${1}_${2}/${NAME}${3}" \
					-ldflags "${LD_FLAGS}" \
					-tags "${GOTAGS}" \
					./cmd/main.go
  endif

  ## build-<os>: os target for building binary executable
  build-$(1):: $(BIN_OUT_DIR)/$1_$2/$(NAME)$(if $(findstring windows,$1),.exe,)
  .PHONY: build-$(1)

  ## build: target for building all os binary executables
  build:: $(BIN_OUT_DIR)/$1_$2/$(NAME)$(if $(findstring windows,$1),.exe,)
endef
$(foreach goarch,$(XC_ARCH),$(foreach goos,$(XC_OS),$(eval $(call make-xc-target,$(goos),$(goarch),$(if $(findstring windows,$(goos)),.exe,)))))

## docker-build: building and pushing multiarch docker image (If you need to publish use PUSH_MULTIARCH=true)
docker-build: build _prepare-multiarch
ifdef PODMAN_EXISTS
	@echo "==> Building multi-arch images with Podman"
	podman build \
		--rm \
		--force-rm \
		--no-cache \
		--compress \
		--file="${PROJ_DIR}/Dockerfile" \
		--platform=${DOCKER_SUPPORTED_PLATFORMS} \
		--build-arg="NAME=${NAME}" \
		--tag="${REPO}/${NAME}" \
		--tag="${REPO}/${NAME}:${VERSION}" \
		"${PROJ_DIR}"

	podman push ${REPO}/${NAME}
	podman push ${REPO}/${NAME}:${VERSION}
else
	@echo "==> Building multi-arch images with Docker"
	@docker buildx build \
		--rm \
		--force-rm \
		--no-cache \
		--compress \
		--file="${PROJ_DIR}/Dockerfile" \
		--platform=${DOCKER_SUPPORTED_PLATFORMS} \
		--build-arg="NAME=${NAME}" \
		--tag="${REPO}/${NAME}" \
		--tag="${REPO}/${NAME}:${VERSION}" \
		$(BUILDX_ARG_PUSH) \
		"${PROJ_DIR}"
endif
.PHONY: docker-build

# ------------------------------------------------------------------------------------------------------------
# Targets for testing
# ------------------------------------------------------------------------------------------------------------

test:
	@go test -vet=off -timeout 10m ./... -count=1 -race -v -p 1
.PHONY: test

# ------------------------------------------------------------------------------------------------------------
# Targets for clean up
# ------------------------------------------------------------------------------------------------------------

clean:
	rm -rf bin
.PHONY: clean

# ------------------------------------------------------------------------------------------------------------
# Helper rules #
# ------------------------------------------------------------------------------------------------------------
# for starting the buildx container
_prepare-multiarch:
ifndef PODMAN_EXISTS
	@docker buildx inspect | grep 'Driver:' | grep 'docker-container' > /dev/null || { docker buildx create --use --name "${NAME}-builder"; docker buildx inspect --bootstrap; }
endif

# help generator
help:
	@echo 'Usage:'
	@sed -n 's/^[ \t]*##//p' ${MAKEFILE_LIST} | column -t -s ':' |  sed -e 's/^/ /' | sort
.PHONY: help