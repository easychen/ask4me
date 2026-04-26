#!/bin/sh
set -e

# If no config file exists under /data, generate one from environment variables.
# This lets users configure ask4me via -e / --env-file without mounting a file.
if [ ! -f /data/.env ] && [ ! -f /data/ask4me.yaml ] && [ ! -f /data/ask4me.yml ]; then
  env | grep -E "^(ASK4ME_|SERVERCHAN_|APPRISE_|BASE_URL=|API_KEY=)" > /data/.env
fi

exec /usr/local/bin/ask4me "$@"
