# Customizable arguments for Docker build
DOCKER_ARGUMENTS ?=

ifdef DOCKER_GO_DEPENDENCY
	DOCKER_ARGUMENTS += --build-context go-dependency=${DOCKER_GO_DEPENDENCY} --build-arg DOCKER_GO_DEPENDENCY=${DOCKER_GO_DEPENDENCY}
endif

all: clean tools test build gosec

clean:
	rm -rf build/
	rm -rf test-nodes/

test:
	go test ./... -coverpkg=./... -count=1 -coverprofile test-coverage.out

build:
    # cd to directory where main.go exits, hack fix for go bug to embed version control data
    # https://github.com/golang/go/issues/51279
	cd ./cli/ubft && go build -o ../../build/ubft

build-docker:
	docker build ${DOCKER_ARGUMENTS} --file scripts/Dockerfile --tag unicity-bft:local .

gosec:
	gosec -exclude-generated ./...

tools:
	go install github.com/securego/gosec/v2/cmd/gosec@latest

.PHONY: \
	all \
	clean \
	tools \
	test \
	build \
	build-docker \
	gosec
