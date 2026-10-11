#!/usr/bin/env bash
# Back up a stopped Reliquary deployment without stopping PostgreSQL.
set -Eeuo pipefail

usage() {
    cat <<'EOF'
Usage: scripts/backup.sh BACKUP_DIRECTORY

Stop the app and pause edits to the file library before running this script.
The PostgreSQL service must remain running. BACKUP_DIRECTORY must be empty
or absent. The backup contains database.dump, vault/, resources/, a manifest,
and SHA-256 checksums. Configuration and encryption keys are not copied.

Run from the directory containing your Compose configuration. Docker Compose
honors COMPOSE_FILE and COMPOSE_PROJECT_NAME. Optional environment variables:
  VAULT_ROOT               Absolute HOST library directory to copy.
  LOCAL_STORAGE_BASE_DIR   Absolute HOST resource directory to copy.
  APP_SERVICE              Compose application service (default: app).
  DB_SERVICE               Compose PostgreSQL service (default: postgres).
Unset the two directory variables to copy the stopped app's mounted storage.
Example:
  COMPOSE_PROJECT_NAME=reliquary-a VAULT_ROOT=/Users/me/vault-a \
    LOCAL_STORAGE_BASE_DIR=/Users/me/files-a scripts/backup.sh /Users/me/backup
EOF
}

die() { printf 'backup: %s\n' "$*" >&2; exit 1; }
if [[ "${1:-}" == "--help" || "${1:-}" == "-h" ]]; then usage; exit 0; fi
[[ $# -eq 1 && -n "$1" ]] || { usage >&2; exit 2; }
for tool in docker cp find mktemp shasum sed grep; do
    command -v "$tool" >/dev/null 2>&1 || die "Required command is missing: $tool"
done

backup_dir=$1
[[ ! -L "$backup_dir" ]] || die "The backup directory must not be a symlink."
mkdir -p "$backup_dir"
backup_dir=$(cd "$backup_dir" && pwd -P)
[[ -z "$(find "$backup_dir" -mindepth 1 -maxdepth 1 -print -quit)" ]] ||
    die "The backup directory is not empty: $backup_dir"

app_service=${APP_SERVICE:-app}
db_service=${DB_SERVICE:-postgres}
docker compose version >/dev/null
services=$(docker compose config --services)
printf '%s\n' "$services" | LC_ALL=C grep -Fxq "$db_service" ||
    die "Compose does not define the PostgreSQL service: $db_service"
db_id=$(docker compose ps --status running --quiet "$db_service")
[[ -n "$db_id" && "$db_id" != *$'\n'* ]] ||
    die "Exactly one PostgreSQL container must be running."

app_id=
if printf '%s\n' "$services" | LC_ALL=C grep -Fxq "$app_service"; then
    [[ -z "$(docker compose ps --status running --quiet "$app_service")" ]] ||
        die "Stop the application first: docker compose stop $app_service"
    app_id=$(docker compose ps --all --quiet "$app_service")
    [[ "$app_id" != *$'\n'* ]] || die "Only one application container is supported."
fi

container_path() {
    [[ -n "$app_id" ]] || die "Create the stopped app container or specify both HOST directories."
    docker inspect --format '{{range .Config.Env}}{{println .}}{{end}}' "$app_id" |
        sed -n "s/^$1=//p"
}

vault_path=${VAULT_ROOT:-}
resource_path=${LOCAL_STORAGE_BASE_DIR:-}
vault_mode=host
resource_mode=host
if [[ -z "$vault_path" ]]; then vault_mode=container; vault_path=$(container_path VAULT_ROOT); fi
if [[ -z "$resource_path" ]]; then
    resource_mode=container
    resource_path=$(container_path LOCAL_STORAGE_BASE_DIR)
fi

normalize_storage_path() {
    local path=$1 mode=$2
    while [[ "$path" == */ && "$path" != / ]]; do path=${path%/}; done
    [[ "$path" == /* && "$path" != / && "$path" != *$'\n'* ]] ||
        die "Storage paths must be absolute directories other than /."
    case "$path/" in
        *//*|*/./*|*/../*) die "Storage paths must not contain empty, . or .. components." ;;
    esac
    if [[ "$mode" == host ]]; then
        [[ -d "$path" && ! -L "$path" ]] || die "HOST directory does not exist: $path"
        path=$(cd "$path" && pwd -P)
        [[ "$path" != / ]] || die "Refusing to copy the filesystem root."
        case "$backup_dir/" in "$path/"*) die "The backup must be outside the source directory." ;; esac
    else
        local mounted=false mount
        while IFS= read -r mount; do
            case "$path/" in "$mount/"*) mounted=true ;; esac
        done < <(docker inspect --format '{{range .Mounts}}{{println .Destination}}{{end}}' "$app_id")
        [[ "$mounted" == true ]] || die "Container storage must be inside a persistent mount: $path"
    fi
    printf '%s\n' "$path"
}
vault_path=$(normalize_storage_path "$vault_path" "$vault_mode")
resource_path=$(normalize_storage_path "$resource_path" "$resource_mode")

stage=$(mktemp -d "$backup_dir/.backup.XXXXXX")
cleanup() {
    local code=$?
    if [[ -n "${stage:-}" && -d "$stage" ]]; then rm -rf "$stage"; fi
    if (( code != 0 )); then printf 'backup: Failed; no complete backup was produced.\n' >&2; fi
}
trap cleanup EXIT

printf 'Exporting PostgreSQL with pg_dump...\n'
docker compose exec -T "$db_service" sh -eu -c '
    export PGPASSWORD="${POSTGRES_PASSWORD:-}"
    exec pg_dump --host=127.0.0.1 --username="${POSTGRES_USER:-postgres}" \
        --dbname="${POSTGRES_DB:-${POSTGRES_USER:-postgres}}" \
        --format=custom --no-owner --no-acl
' > "$stage/database.dump"
[[ -s "$stage/database.dump" ]] || die "pg_dump produced an empty archive."

copy_source() {
    local path=$1 mode=$2 target=$3
    mkdir "$target"
    if [[ "$mode" == host ]]; then
        cp -pR "$path/." "$target/"
    else
        docker cp "$app_id:$path/." "$target/"
    fi
}
printf 'Copying the file library and uploaded resources...\n'
copy_source "$vault_path" "$vault_mode" "$stage/vault"
copy_source "$resource_path" "$resource_mode" "$stage/resources"
printf 'format=reliquary-backup-v1\n' > "$stage/manifest"
(
    cd "$stage"
    find database.dump manifest vault resources -type f -exec shasum -a 256 {} + > checksums.sha256
)

for item in database.dump vault resources checksums.sha256 manifest; do
    mv "$stage/$item" "$backup_dir/$item"
done
rmdir "$stage"
stage=
printf 'Backup completed: %s\n' "$backup_dir"
