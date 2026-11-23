# AGENTS.md

Инструкции ИИ-агентам для работы в этом репозитории.

## Команды

| Команда | Что делает |
| --- | --- |
| `mise install` | ставит закреплённые инструменты (go 1.27.4, node 24.22.2 из `mise.toml`) |
| `mise run setup` | npm-зависимости `frontend/` и `typespec/` |
| `mise run dev-backend` | API на http://localhost:8080 (при старте создаётся демо-встреча) |
| `mise run dev-frontend` | UI на http://localhost:5173 (vite проксирует `/api` на `:8080`) |
| `mise run lint` | gofmt+go vet, ESLint, проверка формата `main.tsp` |
| `mise run test` | `check-contract` + `go test` + `vitest` |
| `mise run build` | `backend/bin/calendar-api` + `frontend/dist` |
| `mise run check` | **все проверки разом** (то же выполняет CI на каждый push) |
| `mise run codegen` | TypeSpec → `openapi.json` → `frontend/src/api/schema.d.ts` |
| `docker build -t calendar-app .` | единый образ (SPA в Go-бинаре, слушает `$PORT`) |

## Деплой

Docker-сервис на Render создаётся/обновляется через render-MCP
(`.opencode/opencode.json`, требует переменную `RENDER_API_KEY`). Данные
SQLite в контейнере эфемерны — персистентность пока не гарантирована.

## Структура

- `backend/` — Go (только stdlib `net/http` + `database/sql`): `main.go` — роуты/JSON,
  `store.go` — SQLite-хранилище, `slots.go` — генерация интервалов, `migrate.go` —
  применения SQL из `backend/migrations/`
- `frontend/` — Vite + vanilla TypeScript: `src/views/{book,create,mine}.ts`,
  `src/api.ts` — обёртка над сгенерированным SDK
- `typespec/` — контракт API `main.tsp` и результат `tsp-output/…/openapi.json`
- `scripts/check-contract.sh` — сверка маршрутов OpenAPI ↔ Go (входит в `test`)

## Правила (неочевидные из кода)

- **Коммиты — Conventional Commits**: `feat|fix|docs|refactor|test|chore|ci(scope):imperative
  subject`, например `feat(backend): per-owner workday hours`.
- Контракт API правится **только** в `typespec/main.tsp`, затем `mise run codegen`.
  Файлы `typespec/tsp-output/**` и `frontend/src/api/schema.d.ts` — артефакты
  генерации, руками не редактировать; `frontend/src/types.ts` — только ре-экспорт
  схем из `schema.d.ts`.
- Хранилище — SQLite (ADR-0001, `modernc.org/sqlite` без cgo): файл `data/calendar.db`
  (переменная `DATA_PATH`), миграции — нумерованные SQL в `backend/migrations/` через
  `go:embed` + таблица `schema_version`; тесты на `:memory:`. Публичные методы `Store`
  — стабильный интерфейс.
- Занятость интервалов сквозная: брони одного организатора (по email) блокируют
  пересекающиеся интервалы во всех его встречах на ту же дату; границы рабочего дня
  хранятся парой (email, дата), дефолт 09:00–18:00.
- Тесты следуют принципу «верхнего достаточного шва» (ADR-0005): доменное правило
  проверяется на самой высокой границе, где оно полностью выражается; HTTP e2e —
  только конкурентность бронирования и коды статусов, дубли Store-тестов туда не
  переносим.
- В `.opencode/opencode.json` в `permission` не оставлять `ask`: GitHub-экшен
  (`opencode github run`, v1) в headless-режиме навсегда зависает на запросе
  согласия, которому некому ответить (anomalyco/opencode#54477) — все действия,
  до которых может дотянуться агент, явно `allow` или `deny`; триаж-воркфлоу
  к тому же ограничен `timeout-minutes`.
- Не удалять и не переименовывать `.github/workflows/hexlet-check.yml` и репозиторий.

## Agent skills

### Issue tracker

Задачи и спецификации живут в GitHub Issues этого репозитория, все операции —
через `gh` CLI. См. `docs/agents/issue-tracker.md`.

### Triage labels

Канонические дефолтные метки: `needs-triage`, `needs-info`, `ready-for-agent`,
`ready-for-human`, `wontfix`. См. `docs/agents/triage-labels.md`.

### Domain docs

Single-context: `GLOSSARY.md` + `docs/adr/` в корне, читаются перед разведкой
кодовой базы. См. `docs/agents/domain.md`.
