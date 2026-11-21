#!/usr/bin/env bash
# Проверка формата TypeSpec: tsp format не должен менять main.tsp.
# На случай расхождения файл остаётся нетронутым (форматим копию).
set -euo pipefail
cd "$(dirname "$0")/../typespec"

formatted=$(mktemp)
cp main.tsp "$formatted"
npx tsp format main.tsp >/dev/null
if ! diff -q "$formatted" main.tsp >/dev/null; then
  cp "$formatted" main.tsp
  rm "$formatted"
  echo "typespec/main.tsp не отформатирован — выполните: mise run fmt-spec" >&2
  exit 1
fi
rm "$formatted"
echo "typespec lint OK"
