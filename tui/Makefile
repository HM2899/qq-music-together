BINARY := qqmusic-tui

.PHONY: build run test test-race clean package bundles checksums verify-packages

# 禁用 VCS 打戳，兼容源码归档和上级目录 Git 元数据不完整的情况
build:
	go build -buildvcs=false -o $(BINARY) .

run: build
	./$(BINARY)

test:
	go test -buildvcs=false -timeout 300s ./...

test-race:
	go test -buildvcs=false -race -timeout 300s ./...

clean:
	rm -f $(BINARY)

# 发布构建独立于开发 build，绝不复用工作区现成二进制。
ARCH ?= amd64
VERSION ?= 0.0.0
COMMIT ?= $(shell git rev-parse HEAD)
REF ?= HEAD
DIST ?= ../dist

package:
	../packaging/build.sh --arch "$(ARCH)" --version "$(VERSION)" --commit "$(COMMIT)" --output "$(DIST)"

bundles:
	../packaging/bundle.sh --version "$(VERSION)" --ref "$(REF)" --output "$(DIST)"

checksums:
	../packaging/checksums.sh "$(DIST)"

verify-packages:
	../packaging/verify.sh "$(DIST)"
