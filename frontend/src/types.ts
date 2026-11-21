// Типы фронтенда — ре-экспорт из SDK, сгенерированного из OpenAPI-спецификации
// (цепочка: typespec/main.tsp -> tsp compile -> openapi.json ->
// openapi-typescript -> src/api/schema.d.ts). Правки контракта делаются
// только в TypeSpec, ручных моделей здесь больше нет.

import type { components } from "./api/schema";

type Schemas = components["schemas"];

// Границы рабочего дня + привязка (owner, date), как отдаёт GET/PUT /api/workhours.
export type WorkHours = Schemas["WorkHoursView"];
export type Meeting = Schemas["Meeting"];
export type Booking = Schemas["Booking"];
export type BookingView = Schemas["BookingView"];
export type Slot = Schemas["Slot"];
export type SlotStatus = Schemas["SlotStatus"];
export type DayAvailability = Schemas["DayAvailability"];
