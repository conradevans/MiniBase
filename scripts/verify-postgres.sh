#!/usr/bin/env bash
set -euo pipefail

readonly EXPECTED_USER="conradevans"
readonly NETWORK_NAME="reactorlab-data"
readonly VOLUME_NAME="minibase-postgres-data"
readonly CONTAINER_NAME="minibase-postgres"
readonly SECRET_RELATIVE_PATH="secrets/postgres-admin-password"

pass_count=0
fail_count=0

pass() {
  printf 'PASS: %s\n' "$1"
  pass_count=$((pass_count + 1))
}

fail() {
  printf 'FAIL: %s\n' "$1" >&2
  fail_count=$((fail_count + 1))
}

finish() {
  printf 'SUMMARY: %d passed, %d failed\n' "$pass_count" "$fail_count"
  if ((fail_count > 0)); then
    exit 1
  fi
}

command -v docker >/dev/null 2>&1 || {
  fail "Docker is installed"
  finish
}
docker info >/dev/null 2>&1 || {
  fail "Docker is available to the current user"
  finish
}
docker compose version >/dev/null 2>&1 || {
  fail "Docker Compose is available"
  finish
}
pass "Docker and Docker Compose are available"

script_dir="$(CDPATH= cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
repo_root="$(CDPATH= cd -- "${script_dir}/.." && pwd -P)"
cd -- "$repo_root"

if docker compose config --quiet; then
  pass "Compose configuration is valid"
else
  fail "Compose configuration is valid"
fi

if docker inspect "$CONTAINER_NAME" >/dev/null 2>&1; then
  pass "PostgreSQL container exists"
else
  fail "PostgreSQL container exists"
  finish
fi

container_running="$(docker inspect --format '{{.State.Running}}' "$CONTAINER_NAME")"
container_health="$(docker inspect --format '{{if .State.Health}}{{.State.Health.Status}}{{else}}none{{end}}' "$CONTAINER_NAME")"
if [[ "$container_running" == "true" ]]; then
  pass "PostgreSQL container is running"
else
  fail "PostgreSQL container is running"
fi
if [[ "$container_health" == "healthy" ]]; then
  pass "PostgreSQL container is healthy"
else
  fail "PostgreSQL container is healthy"
fi

container_image_ref="$(docker inspect --format '{{.Config.Image}}' "$CONTAINER_NAME")"
container_image_id="$(docker inspect --format '{{.Image}}' "$CONTAINER_NAME")"
postgres_17_image_id="$(docker image inspect --format '{{.Id}}' postgres:17 2>/dev/null || true)"
if [[ "$container_image_ref" == "postgres:17" || ( -n "$postgres_17_image_id" && "$container_image_id" == "$postgres_17_image_id" ) ]]; then
  pass "Container uses postgres:17"
else
  fail "Container uses postgres:17"
fi

if docker exec "$CONTAINER_NAME" pg_isready -U minibase_admin -d postgres >/dev/null 2>&1; then
  pass "pg_isready succeeds"
else
  fail "pg_isready succeeds"
fi

select_result="$(docker exec "$CONTAINER_NAME" psql -X -v ON_ERROR_STOP=1 -U minibase_admin -d postgres -tAc 'SELECT 1' 2>/dev/null | tr -d '[:space:]' || true)"
if [[ "$select_result" == "1" ]]; then
  pass "SELECT 1 succeeds"
else
  fail "SELECT 1 succeeds"
fi

if docker network inspect "$NETWORK_NAME" >/dev/null 2>&1; then
  pass "Private Docker network exists"
  network_driver="$(docker network inspect --format '{{.Driver}}' "$NETWORK_NAME")"
  network_internal="$(docker network inspect --format '{{.Internal}}' "$NETWORK_NAME")"
  if [[ "$network_driver" == "bridge" && "$network_internal" == "true" ]]; then
    pass "Private Docker network is an internal bridge"
  else
    fail "Private Docker network is an internal bridge"
  fi
else
  fail "Private Docker network exists"
fi

container_networks="$(docker inspect --format '{{range $name, $_ := .NetworkSettings.Networks}}{{$name}}{{"\n"}}{{end}}' "$CONTAINER_NAME" | sed '/^$/d')"
if [[ "$container_networks" == "$NETWORK_NAME" ]]; then
  pass "PostgreSQL is attached only to ${NETWORK_NAME}"
else
  fail "PostgreSQL is attached only to ${NETWORK_NAME}"
fi

port_binding_count="$(docker inspect --format '{{len .HostConfig.PortBindings}}' "$CONTAINER_NAME")"
published_port="$(docker port "$CONTAINER_NAME" 5432/tcp 2>/dev/null || true)"
if [[ "$port_binding_count" == "0" && -z "$published_port" ]]; then
  pass "PostgreSQL has no published host port"
else
  fail "PostgreSQL has no published host port"
fi

if docker volume inspect "$VOLUME_NAME" >/dev/null 2>&1; then
  pass "Persistent PostgreSQL volume exists"
else
  fail "Persistent PostgreSQL volume exists"
fi

data_mount="$(docker inspect --format '{{range .Mounts}}{{if eq .Destination "/var/lib/postgresql/data"}}{{.Type}}|{{.Name}}|{{.RW}}{{end}}{{end}}' "$CONTAINER_NAME")"
if [[ "$data_mount" == "volume|${VOLUME_NAME}|true" ]]; then
  pass "PostgreSQL data directory uses the expected named volume"
else
  fail "PostgreSQL data directory uses the expected named volume"
fi

compose_model="$(docker compose config)"
volume_external="$(awk '
  /^volumes:$/ { in_volumes=1; next }
  in_volumes && /^[^[:space:]]/ { in_volumes=0; in_target=0 }
  in_volumes && /^  minibase-postgres-data:$/ { in_target=1; next }
  in_target && /^  [^[:space:]]/ { in_target=0 }
  in_target && $1 == "external:" && $2 == "true" { print "true"; exit }
' <<<"$compose_model")"
if [[ "$volume_external" == "true" ]]; then
  pass "Compose declares the PostgreSQL volume external"
else
  fail "Compose declares the PostgreSQL volume external"
fi

password_file="${repo_root}/${SECRET_RELATIVE_PATH}"
if [[ -f "$password_file" && ! -L "$password_file" ]]; then
  pass "Administrator credential file exists as a regular file"

  secret_mode="$(stat -c '%a' "$password_file")"
  if [[ "$secret_mode" == "600" || "$secret_mode" == "400" ]]; then
    pass "Administrator credential permissions are 0600 or stricter"
  else
    fail "Administrator credential permissions are 0600 or stricter"
  fi

  if [[ "$(stat -c '%u' "$password_file")" == "$(id -u "$EXPECTED_USER")" ]]; then
    pass "Administrator credential is owned by ${EXPECTED_USER}"
  else
    fail "Administrator credential is owned by ${EXPECTED_USER}"
  fi
else
  fail "Administrator credential file exists as a regular file"
fi

if git check-ignore -q -- "$SECRET_RELATIVE_PATH" && ! git ls-files --error-unmatch "$SECRET_RELATIVE_PATH" >/dev/null 2>&1; then
  pass "Administrator credential is ignored and untracked"
else
  fail "Administrator credential is ignored and untracked"
fi

password_encryption="$(docker exec "$CONTAINER_NAME" psql -X -v ON_ERROR_STOP=1 -U minibase_admin -d postgres -tAc 'SHOW password_encryption' 2>/dev/null | tr -d '[:space:]' || true)"
if [[ "$password_encryption" == "scram-sha-256" ]]; then
  pass "PostgreSQL password encryption uses SCRAM-SHA-256"
else
  fail "PostgreSQL password encryption uses SCRAM-SHA-256"
fi

host_trust_rules="$(docker exec "$CONTAINER_NAME" psql -X -v ON_ERROR_STOP=1 -U minibase_admin -d postgres -tAc "SELECT count(*) FROM pg_hba_file_rules WHERE type = 'host' AND auth_method = 'trust'" 2>/dev/null | tr -d '[:space:]' || true)"
if [[ "$host_trust_rules" == "0" ]]; then
  pass "PostgreSQL network host authentication does not use trust"
else
  fail "PostgreSQL network host authentication does not use trust"
fi

privileged="$(docker inspect --format '{{.HostConfig.Privileged}}' "$CONTAINER_NAME")"
network_mode="$(docker inspect --format '{{.HostConfig.NetworkMode}}' "$CONTAINER_NAME")"
docker_socket_mount="$(docker inspect --format '{{range .Mounts}}{{if or (eq .Source "/var/run/docker.sock") (eq .Destination "/var/run/docker.sock")}}present{{end}}{{end}}' "$CONTAINER_NAME")"
if [[ "$privileged" == "false" ]]; then
  pass "PostgreSQL is not privileged"
else
  fail "PostgreSQL is not privileged"
fi
if [[ "$network_mode" != "host" ]]; then
  pass "PostgreSQL does not use host networking"
else
  fail "PostgreSQL does not use host networking"
fi
if [[ -z "$docker_socket_mount" ]]; then
  pass "PostgreSQL does not mount the Docker socket"
else
  fail "PostgreSQL does not mount the Docker socket"
fi

if git diff --check; then
  pass "Git diff whitespace check passes"
else
  fail "Git diff whitespace check passes"
fi

finish
