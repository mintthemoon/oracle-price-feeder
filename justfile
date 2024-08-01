export GIT_COMMIT := `git log -1 --format='%H' 2>/dev/null | head -c 12 || exit 0`
export GIT_BRANCH := `git branch --show-current 2>/dev/null || exit 0`
export GIT_TAG := `git describe --exact-match 2>/dev/null || exit 0`
export VERSION := if GIT_TAG != "" { GIT_TAG } else { GIT_BRANCH + "-" + GIT_COMMIT }
export CGO_ENABLED := if os() == "linux" { "1" } else { "0" }

bin_name := "price-feeder"
build_path := "./build"
dockerfile_path := "./__docker__/Dockerfile"
docker_tag := "price-feeder:" + VERSION
docker_args := "--no-cache --progress plain -t " + docker_tag + " -f " + dockerfile_path
ldflags := '''
  -X price-feeder/cmd.Version=${VERSION} \
  -X price-feeder/cmd.Commit=${GIT_COMMIT} \
	-X github.com/cosmos/cosmos-sdk/version.Name=kujira \
	-X github.com/cosmos/cosmos-sdk/version.ServerName=price-feeder \
	-X github.com/cosmos/cosmos-sdk/version.Version=${VERSION} \
	-X github.com/cosmos/cosmos-sdk/version.Commit=${GIT_COMMIT} 
'''

alias b := build
alias i := install
alias t := test
alias docker := docker-build

_list:
  @just --list --no-aliases

# build binary for current platform
build out=bin_name:
  mkdir -p {{build_path}}
  go build -mod=readonly -o "{{build_path}}/{{out}}" -ldflags "{{ldflags}}" main.go

# build binary and install to GOPATH
install:
  go install -mod=readonly -ldflags "{{ldflags}}" ./...

# remove build artifacts
clean:
  rm -rf {{build_path}}

# clean then build
clobber: clean build

# check code with golangci-lint
lint:
  go run github.com/golangci/golangci-lint/cmd/golangci-lint run --timeout=10m

# run all tests
test: unit-test

# build for given platform
[group("build")]
build-platform $GOOS="linux" $GOARCH="amd64" $CC="gcc":
  @echo "Building: os=${GOOS},  arch=${GOARCH}, CC=${CC}"
  @just build "{{bin_name}}_{{GOOS}}-{{GOARCH}}"

# build for linux/amd64
[group("build")]
build-linux-amd64: (build-platform "linux" "amd64" "x86_64-linux-gnu-gcc")

# build for linux/arm64
[group("build")]
build-linux-arm64: (build-platform "linux" "arm64" "aarch64-linux-gnu-gcc")

# build docker image for current platform
[group("docker")]
docker-build:
  docker build {{docker_args}} .

# build docker image for given platform
[group("docker")]
docker-build-platform platform="linux/amd64":
  docker buildx build --platform "{{platform}}" {{docker_args}} .

# build docker image for linux/amd64 and linux/arm64
[group("docker")]
docker-build-multiplatform:
  docker buildx build --platform "linux/amd64,linux/arm64" {{ docker_args }} .

# build, tag, and push docker image (for pipelines)
[group("docker")]
docker-ci repo: docker-build-multiplatform
  just docker-build-multiplatform
  docker tag {{docker_tag}} {{repo}}:{{VERSION}}
  docker tag {{docker_tag}} {{repo}}:{{GIT_BRANCH}}
  docker push {{repo}}:{{VERSION}}
  docker push {{repo}}:{{GIT_BRANCH}}

# run unit tests
[group("tests")]
unit-test:
  go test -mod=readonly -race ./...
