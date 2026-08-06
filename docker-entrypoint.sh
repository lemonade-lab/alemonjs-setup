#!/usr/bin/env sh
set -eu

# Bind mounts are created as root by Docker on many Linux hosts. Initialise
# them before dropping privileges so a fresh `docker compose up` is usable.
mkdir -p /workspace/robots /home/alx/.config /home/alx/.ssh
chown -R alx:alx /workspace /home/alx/.config /home/alx/.ssh
chmod 700 /home/alx/.ssh

exec gosu alx "$@"
