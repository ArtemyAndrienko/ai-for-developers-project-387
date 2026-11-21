#!/usr/bin/env bash
# Цепочка контракта: TypeSpec (typespec/main.tsp) -> OpenAPI 3 (tsp-output)
# -> маршруты Go-сервера (backend/main.go).
# Сверка двусторонняя, по путям И методам:
#  1) каждая операция OpenAPI (путь + метод) должна попадать в mux-шаблон Go
#     с тем же методом; методless-шаблон Go покрывает любой метод своего пути;
#  2) каждый mux-шаблон Go должен описываться хотя бы одной операцией OpenAPI
#     (шаблон с методом — операцией именно этого метода).
# {param}-сегменты в Go ({id}, {sub}) матчат любой непустой сегмент.
set -euo pipefail
cd "$(dirname "$0")/.."

SPEC="typespec/tsp-output/@typespec/openapi3/openapi.json"
if [[ ! -f "$SPEC" ]]; then
  echo "нет $SPEC — выполните: mise run codegen" >&2
  exit 1
fi

node -e '
const fs = require("fs");
const spec = require("./typespec/tsp-output/@typespec/openapi3/openapi.json");
const go = fs.readFileSync("backend/main.go", "utf8");

const HTTP_METHODS = ["get", "put", "post", "delete", "patch"];

const toRegex = (tpl) =>
  new RegExp("^" + tpl.split("/").map((seg) => (seg.startsWith("{") ? "[^/]+" : seg.replace(/[.*+?^${}()|[\]\\]/g, "\\$&"))).join("/") + "$");

// Операции OpenAPI: (метод, путь).
const specOps = [];
for (const [p, item] of Object.entries(spec.paths)) {
  for (const m of Object.keys(item)) {
    if (HTTP_METHODS.includes(m.toLowerCase())) {
      specOps.push({ m: m.toUpperCase(), p, re: toRegex(p) });
    }
  }
}

// mux-шаблоны Go: метод необязателен ("" = любой метод этого пути).
const goPatterns = [
  ...new Set(
    [...go.matchAll(/"(?:(GET|POST|PUT|DELETE|PATCH) )?(\/api\/[^"]*)"/g)].map((m) => (m[1] ?? "") + " " + m[2]),
  ),
].map((s) => {
  const i = s.indexOf(" ");
  return { m: s.slice(0, i), p: s.slice(i + 1), re: toRegex(s.slice(i + 1)) };
});

const label = (m, p) => (m ? m + " " : "") + p;

let bad = 0;
for (const op of specOps) {
  if (!goPatterns.some((g) => g.re.test(op.p) && (g.m === "" || g.m === op.m))) {
    console.error("операция OpenAPI не обслуживается Go: " + label(op.m, op.p));
    bad++;
  }
}
for (const g of goPatterns) {
  if (!specOps.some((op) => g.re.test(op.p) && (g.m === "" || g.m === op.m))) {
    console.error("mux-шаблон Go не описан в OpenAPI: " + label(g.m, g.p));
    bad++;
  }
}
if (bad) process.exit(1);
console.log(`Контракт OK: ${specOps.length} операций OpenAPI покрыты ${goPatterns.length} mux-шаблонами Go`);
'
