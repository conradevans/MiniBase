#!/usr/bin/env bash
set -euo pipefail

readonly EXPECTED_USER="conradevans"
readonly NETWORK_NAME="reactorlab-data"
readonly VOLUME_NAME="minibase-postgres-data"
readonly CONTAINER_NAME="minibase-postgres"
readonly SECRET_RELATIVE_PATH="secrets/postgres-admin-password"
readonly HEALTH_TIMEOUT_SECONDS=120

info() {
  printf 'INFO: %s\n' "$1"
}

die() {
  printf 'ERROR: %s\n' "$1" >&2
  exit 1
}

if [[ "$(id -un)" != "$EXPECTED_USER" ]]; then
  die "run this script as ${EXPECTED_USER}"
fi

command -v docker >/dev/null 2>&1 || die "Docker is not installed"
docker info >/dev/null 2>&1 || die "Docker is unavailable to the current user"
docker compose version >/dev/null 2>&1 || die "Docker Compose is unavailable"

script_dir="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
repo_root="$(CDPATH= cd -- "${script_dir}/.." && pwd -P)"
cd -- "$repo_root"

if docker network inspect "$NETWORK_NAME" >/dev/null 2>&1; then
  network_driver="$(docker network inspect --format '{{.Driver}}' "$NETWORK_NAME")"
  network_internal="$(docker network inspect --format '{{.Internal}}' "$NETWORK_NAME")"
  if [[ "$network_driver" != "bridge" || "$network_internal" != "true" ]]; then
    die "existing ${NETWORK_NAME} network is not an internal bridge; refusing to modify it"
  fi
  info "preserving existing internal bridge network ${NETWORK_NAME}"
else
  docker network create --driver bridge --internal "$NETWORK_NAME" >/dev/null
  info "created internal bridge network ${NETWORK_NAME}"
fi

if docker volume inspect "$VOLUME_NAME" >/dev/null 2>&1; then
  info "preserving existing volume ${VOLUME_NAME}"
else
  docker volume create "$VOLUME_NAME" >/dev/null
  info "created volume ${VOLUME_NAME}"
fi

if [[ -L secrets ]]; then
  die "secrets path must not be a symbolic link"
fi
mkdir -p -- secrets
chmod 700 -- secrets
if [[ "$(stat -c '%u' secrets)" != "$(id -u)" ]]; then
  die "secrets directory is not owned by ${EXPECTED_USER}"
fi

password_file="${repo_root}/${SECRET_RELATIVE_PATH}"
secret_tmp=""
cleanup() {
  if [[ -n "$secret_tmp" && -e "$secret_tmp" ]]; then
    rm -f -- "$secret_tmp"
  fi
}
trap cleanup EXIT

if [[ -e "$password_file" || -L "$password_file" ]]; then
  [[ -f "$password_file" && ! -L "$password_file" ]] || die "existing password path is not a regular file"
  [[ -s "$password_file" ]] || die "existing password file is empty; refusing to overwrite it"
  if [[ "$(stat -c '%u' "$password_file")" != "$(id -u)" ]]; then
    die "existing password file is not owned by ${EXPECTED_USER}"
  fi
  chmod 600 -- "$password_file"
  info "preserving existing PostgreSQL administrator credential"
else
  command -v openssl >/dev/null 2>&1 || die "OpenSSL is required to generate the administrator credential"
  secret_tmp="$(mktemp "${repo_root}/secrets/.postgres-admin-password.XXXXXX")"
  chmod 600 -- "$secret_tmp"
  openssl rand -hex 32 >"$secret_tmp"
  [[ -s "$secret_tmp" ]] || die "credential generation failed"

  if ! ln -- "$secret_tmp" "$password_file" 2>/dev/null; then
    die "password path appeared during generation; refusing to overwrite it"
  fi
  rm -f -- "$secret_tmp"
  secret_tmp=""
  chmod 600 -- "$password_file"
  info "generated a new PostgreSQL administrator credential"
fi

docker compose config --quiet
info "Compose configuration is valid"

docker compose up -d postgres

deadline=$((SECONDS + HEALTH_TIMEOUT_SECONDS))
while ((SECONDS < deadline)); do
  health_status="$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "$CONTAINER_NAME" 2>/dev/null || true)"
  case "$health_status" in
    healthy)
      break
      ;;
    unhealthy)
      die "PostgreSQL reported an unhealthy status"
      ;;
  esac
  sleep 2
done

if [[ "${health_status:-}" != "healthy" ]]; then
  die "PostgreSQL did not become healthy within ${HEALTH_TIMEOUT_SECONDS} seconds"
fi

docker exec "$CONTAINER_NAME" pg_isready -U minibase_admin -d postgres >/dev/null 2>&1 || die "pg_isready failed"
select_result="$(docker exec "$CONTAINER_NAME" psql -X -v ON_ERROR_STOP=1 -U minibase_admin -d postgres -tAc 'SELECT 1' 2>/dev/null | tr -d '[:space:]')"
[[ "$select_result" == "1" ]] || die "PostgreSQL SELECT 1 sanity check failed"

info "PostgreSQL is healthy and the SELECT 1 sanity check passed"
info "MiniBase PostgreSQL bootstrap completed successfully"
