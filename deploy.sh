#!/usr/bin/env bash

set -o pipefail -o xtrace -o errexit

# Docker requires root on my system. I awkwardly have to propagate config to the
# root user, but use kubectl configured as my normal user.
sudo PATH="$PATH" KO_DOCKER_REPO=docker.io/spencerjp $(which ko) resolve -f \
	conf/slowdns.yaml --platform=linux/arm64 | kubectl apply -f -
