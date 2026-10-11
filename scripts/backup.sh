#!/usr/bin/env bash
# Compatibility entry point for reliquary-admin.
set -Eeuo pipefail
admin_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
exec "$admin_root/reliquary-admin" backup "$@"
