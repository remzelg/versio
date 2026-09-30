VERSION := 0.1.0
DEV_ROOT := .dev-install
MANIFEST_URL := http://127.0.0.1:8080/versio/stable/manifest.json
# The public half of the release signing key, created by `make -C ../server keys`.
PUBLIC_KEY_FILE := ../server/.keys/versio.pub
# `make install` targets: the command on PATH, and the launcher's default root.
INSTALL_BIN := $(HOME)/.local/bin
INSTALL_ROOT := $(HOME)/.versio
GO ?= go

ifeq ($(OS),Windows_NT)
EXE := .exe
endif

# Read at recipe time, after the key check has run.
LAUNCHER_LDFLAGS = -X main.manifestURL=$(MANIFEST_URL) -X main.publicKey=$(shell cat $(PUBLIC_KEY_FILE))

# $(call seed,<root>) installs release $(VERSION) into <root> and makes it
# active. This is the only place the payload is built.
seed = mkdir -p $(1)/releases/$(VERSION) \
	&& $(GO) build -ldflags "-X main.version=$(VERSION)" -o $(1)/releases/$(VERSION)/versio$(EXE) ./cmd/versio \
	&& printf '{\n  "active_version": "$(VERSION)"\n}\n' > $(1)/current.json

.PHONY: build install-dev run version install uninstall test clean

# bin/versio is the launcher: the command users run. The payload it starts is
# never placed in bin/; it is only built into a release directory (see seed).
build:
	@test -f $(PUBLIC_KEY_FILE) || (echo "missing $(PUBLIC_KEY_FILE); run: make -C ../server keys" >&2; exit 1)
	mkdir -p bin
	$(GO) build -ldflags "$(LAUNCHER_LDFLAGS)" -o bin/versio$(EXE) ./cmd/versio-launcher

# Reset the dev install to a single release, $(VERSION).
install-dev: build
	rm -rf $(DEV_ROOT)
	$(call seed,$(DEV_ROOT))

# run and version keep the dev install as is, so updates persist between runs.
run: build
	./bin/versio$(EXE) --root $(DEV_ROOT)

version: build
	./bin/versio$(EXE) --root $(DEV_ROOT) --version

# Put `versio` on PATH, using the default root. An existing install keeps its
# releases and state (including updates); only the launcher is replaced.
install: build
	mkdir -p $(INSTALL_BIN)
	cp bin/versio$(EXE) $(INSTALL_BIN)/versio$(EXE)
	test -f $(INSTALL_ROOT)/current.json || ( $(call seed,$(INSTALL_ROOT)) )
	@case ":$$PATH:" in *":$(INSTALL_BIN):"*) ;; *) echo "note: $(INSTALL_BIN) is not on PATH" >&2;; esac

uninstall:
	rm -f $(INSTALL_BIN)/versio$(EXE)
	rm -rf $(INSTALL_ROOT)

test:
	$(GO) test ./...

clean:
	rm -rf bin $(DEV_ROOT)
