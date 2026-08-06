#!/usr/bin/env sh
set -eu

# Bind mounts are created as root by Docker on many Linux hosts. Initialise
# them before dropping privileges so a fresh `docker compose up` is usable.
mkdir -p /workspace/robots /home/albs/.config /home/albs/.ssh
chown -R albs:albs /workspace /home/albs/.config /home/albs/.ssh
chmod 700 /home/albs/.ssh

exec gosu albs "$@"
