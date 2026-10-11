#!/usr/bin/env bash
# Restore a Reliquary backup into a stopped deployment with empty storage.
set -Eeuo pipefail

usage() {
    cat <<'EOF'
Usage: scripts/restore.sh BACKUP_DIRECTORY

The app must be stopped and PostgreSQL running. Restore only into a new
application database and empty library/resource directories. Existing
application tables or files cause an error. Configuration and encryption
keys must be transferred separately before starting the app.

Run from the directory containing your Compose configuration. Docker Compose
honors COMPOSE_FILE and COMPOSE_PROJECT_NAME. Optional environment variables:
  VAULT_ROOT               Absolute HOST library destination.
  LOCAL_STORAGE_BASE_DIR   Absolute HOST resource destination.
  APP_SERVICE              Compose application service (default: app).
  DB_SERVICE               Compose PostgreSQL service (default: postgres).
Unset the directory variables to restore the stopped app's mounted storage.
Example:
  COMPOSE_PROJECT_NAME=reliquary-b VAULT_ROOT=/Users/me/vault-b \
    LOCAL_STORAGE_BASE_DIR=/Users/me/files-b scripts/restore.sh /Users/me/backup
If copying fails after the database commit, keep the app stopped and retry
with a fresh empty database and empty storage after checking the error.
EOF
}

die() { printf 'restore: %s\n' "$*" >&2; exit 1; }
if [[ "${1:-}" == "--help" || "${1:-}" == "-h" ]]; then usage; exit 0; fi
[[ $# -eq 1 && -n "$1" ]] || { usage >&2; exit 2; }
for tool in docker cp find shasum sed grep dirname basename; do
    command -v "$tool" >/dev/null 2>&1 || die "Required command is missing: $tool"
done
[[ -d "$1" && ! -L "$1" ]] || die "Backup directory does not exist or is a symlink."
backup_dir=$(cd "$1" && pwd -P)
[[ -f "$backup_dir/manifest" &&
    "$(cat "$backup_dir/manifest")" == "format=reliquary-backup-v1" &&
    -s "$backup_dir/database.dump" && -s "$backup_dir/checksums.sha256" &&
    -d "$backup_dir/vault" && -d "$backup_dir/resources" ]] ||
    die "The directory is not a complete Reliquary backup."
printf 'Verifying backup checksums...\n'
(cd "$backup_dir" && shasum -a 256 --check checksums.sha256) ||
    die "Backup checksum verification failed."

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
app_image=
if [[ "$vault_mode" == container || "$resource_mode" == container ]]; then
    app_image=$(docker inspect --format '{{.Image}}' "$app_id")
fi

normalize_storage_path() {
    local path=$1 mode=$2 ancestor base suffix=
    while [[ "$path" == */ && "$path" != / ]]; do path=${path%/}; done
    [[ "$path" == /* && "$path" != / && "$path" != *$'\n'* ]] ||
        die "Storage paths must be absolute directories other than /."
    case "$path/" in
        *//*|*/./*|*/../*) die "Storage paths must not contain empty, . or .. components." ;;
    esac
    if [[ "$mode" == host ]]; then
        [[ ! -L "$path" ]] || die "The destination must not be a symlink: $path"
        ancestor=$path
        while [[ ! -e "$ancestor" ]]; do
            [[ ! -L "$ancestor" ]] || die "The destination parent is a dangling symlink: $ancestor"
            suffix="/$(basename "$ancestor")$suffix"
            ancestor=$(dirname "$ancestor")
        done
        [[ -d "$ancestor" ]] || die "The destination parent is not a directory: $ancestor"
        base=$(cd "$ancestor" && pwd -P)
        if [[ "$base" == / && -n "$suffix" ]]; then path=$suffix; else path="$base$suffix"; fi
        [[ "$path" != / ]] || die "Refusing to restore into the filesystem root."
        case "$path/" in "$backup_dir/"*) die "The destination must be outside the backup." ;; esac
        case "$backup_dir/" in "$path/"*) die "The destination must not contain the backup." ;; esac
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
if [[ "$vault_mode" == "$resource_mode" ]]; then
    case "$vault_path/" in "$resource_path/"*) die "Library and resource destinations must not overlap." ;; esac
    case "$resource_path/" in "$vault_path/"*) die "Library and resource destinations must not overlap." ;; esac
fi

check_destination() {
    local path=$1 mode=$2
    if [[ "$mode" == host ]]; then
        if [[ -e "$path" ]]; then
            [[ -d "$path" ]] || die "The destination is not a directory: $path"
            [[ -z "$(find "$path" -mindepth 1 -maxdepth 1 -print -quit)" ]] ||
                die "The destination is not empty: $path"
        fi
    else
        docker run --rm --user 0:0 --volumes-from "$app_id" --entrypoint sh "$app_image" -eu -c '
            test -d "$1" && test ! -L "$1" &&
                test -z "$(find "$1" -mindepth 1 -maxdepth 1 -print -quit)"
        ' sh "$path" || die "The mounted destination is missing or not empty: $path"
    fi
}
check_destination "$vault_path" "$vault_mode"
check_destination "$resource_path" "$resource_mode"

relations=$(docker compose exec -T "$db_service" sh -eu -c '
    export PGPASSWORD="${POSTGRES_PASSWORD:-}"
    exec psql --no-psqlrc --host=127.0.0.1 --username="${POSTGRES_USER:-postgres}" \
        --dbname="${POSTGRES_DB:-${POSTGRES_USER:-postgres}}" --set=ON_ERROR_STOP=1 --tuples-only --no-align
' <<'SQL'
SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
WHERE n.nspname NOT IN ('pg_catalog','information_schema')
  AND n.nspname NOT LIKE 'pg_toast%' AND n.nspname NOT LIKE 'pg_temp_%'
  AND c.relkind IN ('r','p','v','m','S','f')
  AND NOT EXISTS (
    SELECT 1 FROM pg_depend d WHERE d.classid='pg_class'::regclass
      AND d.objid=c.oid AND d.deptype='e'
  );
SQL
)
[[ "$relations" == 0 ]] || die "The destination database already contains application relations."

trap 'code=$?; if (( code != 0 )); then printf "restore: Failed; keep the app stopped and inspect storage before retrying.\n" >&2; fi' EXIT
printf 'Restoring PostgreSQL with pg_restore...\n'
docker compose exec -T "$db_service" sh -eu -c '
    export PGPASSWORD="${POSTGRES_PASSWORD:-}"
    exec pg_restore --host=127.0.0.1 --username="${POSTGRES_USER:-postgres}" \
        --dbname="${POSTGRES_DB:-${POSTGRES_USER:-postgres}}" \
        --clean --if-exists --single-transaction --exit-on-error --no-owner --no-acl
' < "$backup_dir/database.dump"

copy_destination() {
    local source=$1 path=$2 mode=$3
    if [[ "$mode" == host ]]; then
        mkdir -p "$path"
        cp -pR "$source/." "$path/"
    else
        docker cp "$source/." "$app_id:$path/"
        docker run --rm --user 0:0 --volumes-from "$app_id" --entrypoint sh "$app_image" -eu -c '
            if id appuser >/dev/null 2>&1; then chown -R appuser:appuser "$1"; fi
        ' sh "$path"
    fi
}
printf 'Restoring the file library and uploaded resources...\n'
copy_destination "$backup_dir/vault" "$vault_path" "$vault_mode"
copy_destination "$backup_dir/resources" "$resource_path" "$resource_mode"
printf 'Restore completed. Check configuration and encryption keys, then start the app.\n'
