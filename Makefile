NAME := "helm-image"
VERSION := $(shell sed -n -e 's/version:[ "]*\([^"]*\).*/\1/p' plugin.yaml)
DIST := $(CURDIR)/_dist
LDFLAGS := "-X main.version=${VERSION}"
TAR_LINUX := "${NAME}-linux-amd64.tar.gz"
TAR_WINDOWS := "${NAME}-windows-amd64.tar.gz"
BINARY_LINUX := ${NAME}
BINARY_WINDOWS := "${NAME}.exe"

.PHONY: dist

dist: dist_linux dist_windows

dist_linux:
	rm -rf bin && mkdir -p $(DIST)
	GOOS=linux GOARCH=amd64 go get -t -v ./...
	GOOS=linux GOARCH=amd64 go build -o bin/$(BINARY_LINUX) -ldflags $(LDFLAGS) main.go
	curl -L https://github.com/containerd/containerd/releases/download/v1.6.2/containerd-1.6.2-linux-amd64.tar.gz -o containerd.tar.gz
	tar xvf containerd.tar.gz
	tar czvf $(DIST)/$(TAR_LINUX) bin README.md LICENSE plugin.yaml

.PHONY: dist_windows
dist_windows:
	rm -rf bin && mkdir -p $(DIST)
	GOOS=windows GOARCH=amd64 go get -t -v ./...
	GOOS=windows GOARCH=amd64 go build -o bin/$(BINARY_WINDOWS) -ldflags $(LDFLAGS) main.go
	curl -L https://github.com/containerd/containerd/releases/download/v1.6.2/containerd-1.6.2-windows-amd64.tar.gz -o containerd.tar.gz
	tar xvf containerd.tar.gz
	tar czvf $(DIST)/${TAR_WINDOWS} bin README.md LICENSE plugin.yaml
