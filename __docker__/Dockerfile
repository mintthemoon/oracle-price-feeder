ARG src=remote
ARG remote_branch=master
ARG remote_url="https://github.com/Team-Kujira/oracle-price-feeder.git"
ARG user=kuji
ARG home=/kuji
ARG uid=3232
ARG gid=3232
ARG binary="price-feeder"
ARG just_install_url="https://github.com/casey/just/releases/download/1.33.0/just-1.33.0-x86_64-unknown-linux-musl.tar.gz"

FROM --platform=$BUILDPLATFORM golang:1.21.8-bookworm AS build
ARG just_install_url
ARG binary
ARG TARGETOS
ARG TARGETARCH
ENV DEBIAN_FRONTEND=noninteractive
WORKDIR /work
RUN echo "install just from $just_install_url"
RUN CC_ARCH=$(case "$TARGETARCH" in \
        "amd64") echo "x86_64" ;; \
        "arm64") echo "aarch64" ;; \
        *) echo "unknown" ;; \
    esac) && \
    CC_OS=$(case "$TARGETOS" in \
        "linux") echo "linux-gnu" ;; \
        *) echo "unknown" ;; \
    esac) && \
    apt-get update && \
    apt-get upgrade -y && \
    apt-get install -y curl "gcc-${CC_ARCH}-${CC_OS}" && \
    curl -fsSL "$just_install_url" | tar -xzvC /usr/bin -- just
COPY . .
RUN just build-${TARGETOS}-${TARGETARCH}
WORKDIR /work/dist
RUN mkdir bin lib && \
    mv ../build/$binary ./bin/ && \
    mv $(ldd ./bin/$binary | grep libwasmvm | awk '{print $3}') ./lib/

FROM debian:bookworm AS main
ARG user
ARG home
ARG uid
ARG gid
ARG binary
ENV DEBIAN_FRONTEND=noninteractive
COPY --from=build /work/dist/* /usr/
RUN apt-get update && \
    apt-get upgrade -y && \
    apt-get install -y ca-certificates && \
    groupadd -g "$gid" -r "$user" && \
    useradd -m -u "$uid" -g "$user" -s /bin/bash -d "$home" "$user"
USER ${user}:${user}
WORKDIR $home
ENTRYPOINT ["$binary"]
