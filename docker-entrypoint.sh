#!/bin/sh
set -e

# If a yaml config is mounted, use it (env vars are ignored).
# Otherwise, always regenerate /data/.env from environment variables on every start
# so that updating an env var (e.g. ASK4ME_API_KEY) actually takes effect after redeploy,
# even when /data is a persistent volume.
if [ ! -f /data/ask4me.yaml ] && [ ! -f /data/ask4me.yml ]; then
  if env | grep -qE "^(ASK4ME_|SERVERCHAN_|APPRISE_|BASE_URL=|API_KEY=)"; then
    env | grep -E "^(ASK4ME_|SERVERCHAN_|APPRISE_|BASE_URL=|API_KEY=)" > /data/.env
  fi
fi

exec /usr/local/bin/ask4me "$@"
