export GIT_COMMIT := `git log -1 --format='%H' 2>/dev/null | head -c 12 || exit 0`
export GIT_BRANCH := `git branch --show-current 2>/dev/null || exit 0`
export GIT_TAG := `git describe --exact-match 2>/dev/null || exit 0`
export VERSION := if GIT_TAG != "" { GIT_TAG } else { GIT_BRANCH + "-" + GIT_COMMIT }
export CGO_ENABLED := if os() == "linux" { "1" } else { "0" }

ldflags := '''
  -X price-feeder/cmd.Version=${VERSION} \
  -X price-feeder/cmd.Commit=${GIT_COMMIT} \
	-X github.com/cosmos/cosmos-sdk/version.Name=kujira \
	-X github.com/cosmos/cosmos-sdk/version.ServerName=price-feeder \
	-X github.com/cosmos/cosmos-sdk/version.Version=${VERSION} \
	-X github.com/cosmos/cosmos-sdk/version.Commit=${GIT_COMMIT} 
'''
build_path := "./build"
dockerfile_path := "./__docker__/Dockerfile"
docker_tag := "price-feeder:" + VERSION
docker_args := "--no-cache --progress plain -t " + docker_tag + " -f " + dockerfile_path

alias b := build
alias i := install
alias t := test
alias docker := docker-build

_list:
  @just --list --no-aliases

build:
  mkdir -p {{build_path}}
  go build -mod=readonly -o {{build_path}} -ldflags "{{ldflags}}" ./...

install:
  go install -mod=readonly -ldflags "{{ldflags}}" ./...

clean:
  rm -rf {{build_path}}
  go clean

clobber: clean build

lint:
  go run github.com/golangci/golangci-lint/cmd/golangci-lint run --timeout=10m

test: unit-test

[group("docker")]
docker-build:
  docker build {{docker_args}} .

[group("docker")]
docker-ci REPO: docker-build
  docker tag {{docker_tag}} {{REPO}}:{{VERSION}}
  docker push {{REPO}}:{{VERSION}}

[group("tests")]
unit-test:
  go test -mod=readonly -race ./...
