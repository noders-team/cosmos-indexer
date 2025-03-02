#!/usr/bin/make -f

BRANCH := $(shell git rev-parse --abbrev-ref HEAD)
COMMIT := $(shell git log -1 --format='%H')

# don't override user values
ifeq (,$(VERSION))
  VERSION := $(shell git describe --tags)
  # if VERSION is empty, then populate it with branch's name and raw commit hash
  ifeq (,$(VERSION))
    VERSION := $(BRANCH)-$(COMMIT)
  endif
endif

# default value, overide with: make -e FQCN="foo"
FQCN = ghcr.io/nodersteam/cosmos-indexer

grpc_gen:
	protoc proto/*.proto \
        --go_out=./proto \
        --go_opt=paths=source_relative \
        --go-grpc_out=./proto \
        --go-grpc_opt=paths=source_relative \
        --proto_path=./proto

all: install

install: go.sum
	go install .

up:
	docker-compose up --build

up-dev:
	docker network inspect indexer_network >/dev/null 2>&1 || sudo docker network create indexer_network
	docker compose -f docker-compose-dev.yml up -d --build

reload-dev:
	docker compose -f docker-compose-dev.yml up -d --build --force-recreate

refresh-dev:
	docker compose -f docker-compose-dev.yml down -v
	rm -rf mongodb-data && rm -rf postgres-data
	docker compose -f docker-compose-dev.yml up -d --build

clean:
	rm -rf build

build-docker-amd:
	docker build -t $(FQCN):$(VERSION) -f ./Dockerfile \
	--build-arg TARGETPLATFORM=linux/amd64 .

build-docker-arm:
	docker build -t $(FQCN):$(VERSION) -f ./Dockerfile \
	--build-arg TARGETPLATFORM=linux/arm64 .

.PHONY: lint
lint: ## Run golangci-linter
	golangci-lint run --out-format=tab

.PHONY: format
format: ## Formats the code with gofumpt
	find . -name '*.go' -type f -not -path "./vendor*" -not -path "*.git*" -not -path "./client/docs/*" | xargs gofumpt -w

build_0g:
	cp -f go.mod.0g go.mod
	go mod tidy
	go mod vendor
	go build -o bin/cosmos-indexer .

build_cel:
	cp -f go.mod.cel go.mod
	go mod tidy
	go mod vendor
	go build -o bin/cosmos-indexer .

build:
	cp -f go.mod.or go.mod
	go mod tidy
	go mod vendor
	go build -o bin/cosmos-indexer .

run_dev:
	go build . && \
		./cosmos-indexer index \
		   --log.pretty = true \
		   --log.level = info \
		   --base.start-block 100000 \
		   --base.end-block -1 \
		   --base.throttling 2.005 \
		   --base.rpc-workers 1 \
		   --base.index-transactions true \
		   --probe.rpc http://168.119.208.253:26657  \
		   --base.index-evm-transactions true \
		   --probe.account-prefix bera \
		   --probe.chain-id 0x138de \
		   --probe.chain-name bera \
		   --probe.evm-rpc-url https://berachain-rpc.publicnode.com \
		   --database.host postgres.cosmos-indexer.orb.local \
		   --database.database indexer \
		   --database.user postgres \
		   --database.password password \
		   --server.port 9002 \
		   --redis.addr redis.cosmos-indexer.orb.local:6379 \
		   --mongo.addr mongodb://admin:password@mongodb.cosmos-indexer.orb.local:27017 \
		   --mongo.db search_indexer