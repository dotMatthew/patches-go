BINARY    := patches
DIST      := dist
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64
GOFLAGS   := -trimpath
LDFLAGS   := -s -w

.PHONY: build dist test test-arm64 vet clean

# Binary for the current platform.
build:
	CGO_ENABLED=0 go build $(GOFLAGS) -ldflags="$(LDFLAGS)" -o $(BINARY) .

# Static binaries for all supported platforms in dist/.
dist:
	@mkdir -p $(DIST)
	@for p in $(PLATFORMS); do \
		os=$${p%/*}; arch=$${p#*/}; ext=; [ $$os = windows ] && ext=.exe; \
		echo "building $$os/$$arch"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build $(GOFLAGS) -ldflags="$(LDFLAGS)" \
			-o $(DIST)/$(BINARY)-$$os-$$arch$$ext . || exit 1; \
	done

vet:
	go vet ./...

# Black box tests (needs git); builds the binary itself.
test: vet
	go test -count=1 ./...

# Runs the tests against the linux/arm64 binary under qemu inside Docker.
test-arm64: dist
	docker build -q -t patches-e2e-arm64 -f e2e/Dockerfile.arm64 e2e
	docker run --rm -v "$(CURDIR)":/work -w /work patches-e2e-arm64 \
		sh -c 'PATCHES_CMD="qemu-aarch64-static /work/$(DIST)/$(BINARY)-linux-arm64" go test -count=1 ./e2e'

clean:
	rm -rf $(BINARY) $(BINARY).exe $(DIST)
