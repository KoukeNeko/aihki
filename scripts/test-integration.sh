#!/usr/bin/env sh

set -eu

repository_dir=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)
compose_file="$repository_dir/test/e2e/docker-compose.yml"
project_name="taiga-cli-e2e"
temporary_dir=$(mktemp -d)

cleanup() {
    docker compose --project-name "$project_name" --file "$compose_file" down --volumes --remove-orphans >/dev/null 2>&1 || true
    rm -rf "$temporary_dir"
}

trap cleanup EXIT INT TERM

docker compose --project-name "$project_name" --file "$compose_file" up --detach --wait

attempt=0
until curl --fail --silent --show-error --max-time 5 \
    http://localhost:19000/taiga/api/v1/locales >/dev/null; do
    attempt=$((attempt + 1))
    if [ "$attempt" -ge 60 ]; then
        docker compose --project-name "$project_name" --file "$compose_file" logs --no-color
        exit 1
    fi
    sleep 2
done

(
    cd "$repository_dir"
    go build -o "$temporary_dir/taiga" ./cmd/taiga
    TAIGA_E2E_BIN="$temporary_dir/taiga" \
    TAIGA_E2E_URL="http://localhost:19000/taiga/api/v1/" \
    TAIGA_E2E_HOST="http://localhost:19000/taiga/" \
    go test -tags=integration -count=1 ./test/e2e
)
