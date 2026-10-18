#!/bin/bash
set -e

# ─── Fix ownership of bind-mounted directories ───
# When users bind-mount data directories,
# the mount inherits the host UID/GID which may differ from the
# container's appuser. This entrypoint runs as root, fixes ownership,
# then drops privileges to appuser via gosu — the same pattern used
# by the official PostgreSQL image.

# Directories that may be bind-mounted and need appuser access
MOUNT_DIRS=(
    "${LOCAL_STORAGE_BASE_DIR:-/data/files}"
    "${VAULT_ROOT:-/data/vault}"
    "${RELIQUARY_DIAGNOSTICS_ROOT:-/data/diagnostics}"
)

for dir in "${MOUNT_DIRS[@]}"; do
    if [ -d "$dir" ]; then
        chown -R appuser:appuser "$dir" 2>/dev/null || true
    fi
done

# ─── Drop privileges and exec the main process ───
exec gosu appuser "$@"
