#!/usr/bin/env bash
# Линтер бэкенда: gofmt (проверка без правок) + go vet.
set -euo pipefail
cd "$(dirname "$0")/../backend"

unformatted=$(gofmt -l .)
if [[ -n "$unformatted" ]]; then
  echo "нужен gofmt (mise exec -- gofmt -w .):" >&2
  echo "$unformatted" >&2
  exit 1
fi

go vet ./...
echo "backend lint OK"
