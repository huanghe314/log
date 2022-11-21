# ==============================================================================
# Makefile helper functions for golang
#

SHELL := /bin/bash

GO_SUPPORTED_VERSIONS ?= 1.18|1.19|1.20

FIND := find . ! -path './third_party/*' ! -path './vendor/*'

XARGS := xargs -r

ROOT_DIR := $(abspath $(shell pwd -P))

TOOLS ?= golines golangci-lint goimports

# Build all by default, even if it's not first
.DEFAULT_GOAL := all

.PHONY: all
all: verify format lint test

.PHONY: verify
verify:
ifneq ($(shell go version | grep -q -E '\bgo($(GO_SUPPORTED_VERSIONS))\b' && echo 0 || echo 1), 0)
	$(error unsupported go version. Please make install one of the following supported version: '$(GO_SUPPORTED_VERSIONS)')
endif

.PHONY: tools.install
tools.install: $(addprefix tools.install., $(TOOLS))

.PHONY: tools.install.%
tools.install.%:
	@echo "===========> Installing $*"
	@$(MAKE) install.$*

.PHONY: tools.verify.%
tools.verify.%:
	@if ! which $* &>/dev/null; then $(MAKE) tools.install.$*; fi

.PHONY: install.golangci-lint
install.golangci-lint:
	@go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.48.0
	@golangci-lint completion bash > $(HOME)/.golangci-lint.bash
	@if ! grep -q .golangci-lint.bash $(HOME)/.bashrc; then echo "source \$$HOME/.golangci-lint.bash" >> $(HOME)/.bashrc; fi

.PHONY: install.goimports
install.goimports:
	@go install golang.org/x/tools/cmd/goimports@v0.3.0

.PHONY: install.golines
install.golines:
	@go install github.com/segmentio/golines@v0.11.0

.PHONY: format
format: tools.verify.goimports tools.verify.golines
	@echo "===========> Formating codes"
	@$(FIND) -type f -name '*.go' | $(XARGS) gofmt -s -w
	@$(FIND) -type f -name '*.go' | $(XARGS) goimports -w -local $(ROOT_PACKAGE)
	@$(FIND) -type f -name '*.go' | $(XARGS) golines -w --max-len=120 --reformat-tags --shorten-comments --ignore-generated .
	@go mod edit -fmt

.PHONY: lint
lint: tools.verify.golangci-lint
	@echo "===========> Lint codes"
	@golangci-lint run -c $(ROOT_DIR)/.golangci.yaml $(ROOT_DIR)/...

.PHONY: test
test:
	@echo "===========> Run test"
	@set -o pipefail;
	@go test -race -timeout=10m -shuffle=on -short -v

