.PHONY: build test lint clean prep-assets release-ready ensure-asset-stubs

build:
	./scripts/build-local.sh

ensure-asset-stubs:
	./scripts/ensure-asset-stubs.sh

test: ensure-asset-stubs
	go test -race -cover ./...

lint: ensure-asset-stubs
	gofmt -l $(shell find . -name '*.go')
	go vet ./...

prep-assets:
	./scripts/prep-assets.sh $(GOOS) $(GOARCH)

clean:
	rm -rf dist/
	rm -rf internal/assets/*/

release-ready:
	./scripts/run-release-readiness.sh
