# Makefile for the unifi CLI

BINARY      := unifi
PKG         := ./cmd/unifi
INSTALL_DIR := $(HOME)/bin

# Skill: source lives in the repo; symlinked into the Claude skills dir.
SKILL_NAME  := unifi-network-manager
SKILL_SRC   := $(CURDIR)/skills/$(SKILL_NAME)
SKILL_DIR   := $(HOME)/.claude/skills
SKILL_LINK  := $(SKILL_DIR)/$(SKILL_NAME)

.DEFAULT_GOAL := build
.PHONY: build install install-skill uninstall uninstall-skill test vet fmt clean all help

build: ## Build the ./unifi binary in the repo root
	go build -o $(BINARY) $(PKG)

install: build install-skill ## Build, install the binary to $(INSTALL_DIR), and link the skill
	mkdir -p "$(INSTALL_DIR)"
	install -m 0755 $(BINARY) "$(INSTALL_DIR)/$(BINARY)"
	@echo "Installed $(BINARY) -> $(INSTALL_DIR)/$(BINARY)"

install-skill: ## Symlink the unifi-network-manager skill into ~/.claude/skills
	@mkdir -p "$(SKILL_DIR)"
	@if [ -e "$(SKILL_LINK)" ] && [ ! -L "$(SKILL_LINK)" ]; then \
		echo "Error: $(SKILL_LINK) exists and is not a symlink; remove it first." >&2; exit 1; \
	fi
	@ln -sfn "$(SKILL_SRC)" "$(SKILL_LINK)"
	@echo "Linked skill $(SKILL_LINK) -> $(SKILL_SRC)"

uninstall: uninstall-skill ## Remove the installed binary and the skill symlink
	rm -f "$(INSTALL_DIR)/$(BINARY)"
	@echo "Removed $(INSTALL_DIR)/$(BINARY)"

uninstall-skill: ## Remove the skill symlink (only if it is a symlink)
	@if [ -L "$(SKILL_LINK)" ]; then \
		rm -f "$(SKILL_LINK)"; echo "Removed skill symlink $(SKILL_LINK)"; \
	else \
		echo "No skill symlink at $(SKILL_LINK) (nothing to remove)"; \
	fi

test: ## Run all tests (with the race detector)
	go mod verify
	go test -race ./...

vet: ## Run go vet
	go vet ./...

fmt: ## Format all Go source
	gofmt -w .

clean: ## Remove the locally built binary
	rm -f $(BINARY)

all: fmt vet test build ## Format, vet, test, then build

help: ## List available targets
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  %-12s %s\n", $$1, $$2}'
