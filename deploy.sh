#!/usr/bin/env bash

set -o pipefail -o xtrace -o errexit

CONF="conf/slowdns.yaml"
if [[ -e "private/slowdns.yaml" ]]; then
	CONF="private/slowdns.yaml"
fi

# Docker requires root on my system. I awkwardly have to propagate config to the
# root user, but use kubectl configured as my normal user.
sudo PATH="$PATH" KO_DOCKER_REPO=docker.io/spencerjp $(which ko) resolve -f \
	"$CONF" --platform=linux/arm64 | kubectl apply -f -
