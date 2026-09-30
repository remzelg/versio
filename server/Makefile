KEYS := .keys
DIST := dist
ADDR := 127.0.0.1:8080
GO ?= go

.PHONY: keys release serve test clean

keys: $(KEYS)/versio.key

$(KEYS)/versio.key:
	$(GO) run ./cmd/versio-release keygen -out $(KEYS)

# Publish a signed release: make release RELEASE=0.2.0
release: keys
	@test -n "$(RELEASE)" || (echo "usage: make release RELEASE=X.Y.Z" >&2; exit 2)
	$(GO) run ./cmd/versio-release build -version $(RELEASE) -key $(KEYS)/versio.key -out $(DIST)

serve:
	$(GO) run ./cmd/versio-release serve -dir $(DIST) -addr $(ADDR)

test:
	$(GO) test ./...

# Keys are kept: installed launchers trust them.
clean:
	rm -rf $(DIST)
