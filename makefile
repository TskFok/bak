mac:
	go env -w GOARCH=amd64
	go env -w GOOS=darwin
	go env -w CGO_ENABLED=0
	go env -w GO111MODULE=on
	go env -w GOPROXY=https://goproxy.cn,direct
	go mod  tidy

linux:
	go env -w GOARCH=amd64
	go env -w GOOS=linux
	go env -w CGO_ENABLED=0
	go env -w GO111MODULE=on
	go env -w GOPROXY=https://goproxy.cn,direct
	go mod  tidy

conf-local:
	mv utils/conf/conf.yaml utils/conf/conf.yaml.bak
	mv utils/conf/conf.yaml.local utils/conf/conf.yaml

conf-rollback:
	mv utils/conf/conf.yaml utils/conf/conf.yaml.local
	mv utils/conf/conf.yaml.bak utils/conf/conf.yaml

build-file:
	go build -o bak -ldflags "-w -s"  -trimpath main.go

build-file-cli:
	go build -o cli -ldflags "-w -s"  -trimpath bin/cli/main.go

build-mac: mac conf-local build-file conf-rollback

build-linux: linux conf-local build-file conf-rollback

build-cli-linux: linux conf-local build-file-cli conf-rollback

run-mac: mac
	go run main.go

update:
	go mody tidy

IMAGE ?= bak
BUILDER ?= bak-builder

.PHONY: docker-builder docker-build-linux-amd64 docker-build-linux-arm64

docker-builder:
	docker buildx inspect $(BUILDER) >/dev/null 2>&1 || docker buildx create --name $(BUILDER) --driver docker-container >/dev/null
	docker buildx inspect $(BUILDER) --bootstrap >/dev/null

docker-build-linux-amd64: docker-builder
	docker buildx build --builder $(BUILDER) --platform linux/amd64 --load -t $(IMAGE):linux-amd64 .

docker-build-linux-arm64: docker-builder
	docker buildx build --builder $(BUILDER) --platform linux/arm64 --load -t $(IMAGE):linux-arm64 .
