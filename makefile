conf-local:
	mv utils/conf/conf.yaml utils/conf/conf.yaml.bak
	mv utils/conf/conf.yaml.local utils/conf/conf.yaml

conf-rollback:
	mv utils/conf/conf.yaml utils/conf/conf.yaml.local
	mv utils/conf/conf.yaml.bak utils/conf/conf.yaml

build-file:
	CGO_ENABLED=0 go build -o bak -ldflags "-w -s" -trimpath main.go

build-mac: conf-local
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -o bak -ldflags "-w -s" -trimpath main.go
	$(MAKE) conf-rollback

build-linux: conf-local
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o bak -ldflags "-w -s" -trimpath main.go
	$(MAKE) conf-rollback

run-mac:
	go run main.go

update:
	go mody tidy

IMAGE ?= bak
BUILDER ?= bak-builder

.PHONY: conf-local conf-rollback build-file build-mac build-linux run-mac update docker-builder docker-build-linux-amd64 docker-build-linux-arm64

docker-builder:
	docker buildx inspect $(BUILDER) >/dev/null 2>&1 || docker buildx create --name $(BUILDER) --driver docker-container >/dev/null
	docker buildx inspect $(BUILDER) --bootstrap >/dev/null

docker-build-linux-amd64: docker-builder
	docker buildx build --builder $(BUILDER) --platform linux/amd64 --load -t $(IMAGE):linux-amd64 .

docker-build-linux-arm64: docker-builder
	docker buildx build --builder $(BUILDER) --platform linux/arm64 --load -t $(IMAGE):linux-arm64 .
