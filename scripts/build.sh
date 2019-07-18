#!/bin/bash

set -eo pipefail

VERSION=${1:-master}
GOOS=${2:-linux}
DOCKER_REPO="premiereglobal/vault-to-envs"

# Directory to house our binaries
mkdir -p bin

# Build the binary in Docker
docker build --build-arg GOOS=${GOOS} -t ${DOCKER_REPO}:${VERSION}-${GOOS} ./

# Run the container in the background in order to extract the binary
docker run --rm --entrypoint "" --name vault-to-envs-build -d ${DOCKER_REPO}:${VERSION}-${GOOS} sh -c "sleep 60"

docker cp vault-to-envs-build:/usr/local/bin/v2e bin
docker stop vault-to-envs-build

# Zip up the binary
cd bin
tar -cvzf vault-to-envs-${GOOS}-${VERSION}.tar.gz v2e

# Get us back to the root
cd ..
