// Контракт API «Календарь звонков».
// Цепочка: TypeSpec -> (tsp compile) -> OpenAPI 3 -> (openapi-typescript) ->
// типы/клиент SDK фронтенда (frontend/src/api/schema.d.ts).
// Синхронизировано с backend/main.go и backend/store.go.

import "@typespec/http";
import "@typespec/rest";
import "@typespec/openapi3";

using TypeSpec.Http;
using TypeSpec.Rest;

@service(#{ title: "Календарь звонков API" })
@doc("Бронирование временны́х интервалов встреч. Хранилище — память Go-структур.")
namespace CalendarApi;

// --- Общие типы ---

@doc("Тело ошибки API")
model ErrorBody {
  error: string;
}

@error
model ApiError<Code extends int32 = 400> {
  @statusCode code: Code;
  @body body: ErrorBody;
}

@doc("Границы рабочего дня 'HH:MM'")
model WorkHours {
  workStart: plainTime;
  workEnd: plainTime;
}

@doc("Границы рабочего дня, сопоставленные с парой (email организатора, дата)")
model WorkHoursView extends WorkHours {
  owner: string;
  date: plainDate;

  @doc("IANA-зона Организатора из профиля; без профиля — UTC. В этих часах понимаются workStart/workEnd на дату")
  timezone: string;
}

@doc("Тело PUT /workhours: границы сохраняются только для пары (owner, date); зона — ответная, её просить не нужно")
model WorkHoursInput {
  owner: string;
  date: plainDate;
  workStart: plainTime;
  workEnd: plainTime;
}

@doc("Состояние Встречи: cancelled — терминальное (Отмена Встречи Организатором)")
enum MeetingState {
  @doc("Встреча активна: доступна для записи")
  active,

  @doc("Встреча отменена: исчезает из списков, её Записи отменены")
  cancelled,
}

@doc("Запись на встречу")
model Meeting {
  @key id: string;
  name: string;
  description: string;

  @doc("Длительность интервала в минутах")
  durationMin: int32;

  @doc("Шаг стартов сетки в минутах; отсутствие = длительность Встречи (ADR-0003)")
  startStepMin?: int32;

  @doc("Email организатора: по нему проверяется занятость во всех его встречах")
  ownerEmail: string;

  @doc("Имя организатора, необязательное; видно посетителям")
  ownerName: string;

  @doc("День, на который назначена встреча")
  date: plainDate;

  @doc("Состояние Встречи; отменённая не выдаётся в списках и не бронируется")
  state: MeetingState;

  @doc("IANA-зона Организатора из его профиля: Рабочий день, календарные даты, Окно доступности и вердикт «уже прошло» считаются в ней; без профиля — UTC")
  timezone: string;

  createdAt: utcDateTime;
}

@doc("Тело POST /meetings")
model NewMeeting {
  name: string;
  description?: string;
  durationMin: int32;

  @doc("Шаг стартов: целый 1..durationMin; отсутствие = длительность (422 при выходе за диапазон)")
  startStepMin?: int32;

  ownerEmail: string;
  ownerName?: string;
  date: plainDate;

  @doc("IANA-зона Организатора; при сохранении Встречи профиль создаётся/обновляется, пустое значение — профиль не трогать")
  timezone?: string;
}

enum SlotStatus {
  @doc("Свободен")
  free,

  @doc("Занят этой или другой встречей того же организатора (по пересечению)")
  taken,

  @doc("Время начала уже прошло")
  past,
}

@doc("Временной интервал (интервал) дня")
model Slot {
  start: plainTime;
  end: plainTime;
  status: SlotStatus;
}

@doc("Число свободных интервалов на день (бейдж «18 св.»)")
model DayAvailability {
  date: plainDate;
  free: int32;
}

@doc("Состояние Записи: cancelled — терминальное (Отмена Записи Посетителем)")
enum BookingState {
  @doc("Запись активна: Интервал занят")
  active,

  @doc("Запись отменена: Интервал свободен")
  cancelled,
}

@doc("Бронь посетителем интервала встречи")
model Booking {
  @key id: string;
  meetingId: string;
  date: plainDate;
  start: plainTime;
  end: plainTime;
  name: string;
  email: string;
  state: BookingState;
  createdAt: utcDateTime;
}

@doc("Запись для вкладки «Мои записи»: встреча, организатор (имя и/или email), статус")
model BookingView extends Booking {
  meetingName: string;
  meetingOwnerName: string;
  meetingOwnerEmail: string;

  @doc("Состояние Встречи записи: источник «Отменена» выводится (cancelled у Встречи — отмена Организатором), отдельно не хранится")
  meetingState: MeetingState;

  @doc("Встреча уже закончилась — попадает в группу «Прошедшие»")
  past: boolean;

  @doc("IANA-зона Организатора встречи: в её часах стоят start/end Записи; без профиля — UTC")
  timezone: string;
}

@doc("Тело POST /meetings/{id}/book — день берётся из встречи")
model BookRequest {
  start: plainTime;
  name: string;
  email: string;
}

// --- /api/workhours: границы на конкретный день конкретного организатора ---

@route("/api/workhours")
interface WorkHoursApi {
  @get
  @doc("Границы дня для (owner, date); по умолчанию 09:00–18:00")
  getHours(
    @query @doc("email организатора") owner: string,
    @query @doc("дата встречи") date: plainDate,
  ): WorkHoursView | ApiError<400>;

  @put
  @doc("Сохранить границы только для пары (owner, date)")
  setHours(@body body: WorkHoursInput):
    | WorkHoursView
    | ApiError<400>
    | ApiError<422>;
}

// --- /api/meetings: встречи и бронирование ---

@route("/api/meetings")
interface MeetingsApi {
  @get
  @doc("Список доступных встреч (отменённые исключены); ?owner= фильтрует по организателю")
  list(@query owner?: string): Meeting[] | ApiError<400>;

  @post
  @doc("Создать встречу на конкретный день окна доступности")
  create(@body body: NewMeeting):
    | {
        @statusCode code: 201;
        @body meeting: Meeting;
      }
    | ApiError<422>;

  @route("/{id}")
  @delete
  @doc("Отмена Встречи Организатором (мягкая): Встреча переходит в cancelled, её активные Записи — в «Отменена» одной транзакцией; повторная отмена идемпотентна (тот же 204); 404 — нет такой Встречи")
  remove(@path id: string):
    | {
        @statusCode code: 204;
      }
    | ApiError<404>;

  @route("/{id}/availability")
  @get
  @doc("Свободные интервалы на день встречи; 404 — встречи нет или она отменена (не в продаже)")
  availability(@path id: string): DayAvailability | ApiError<404>;

  @route("/{id}/slots")
  @get
  @doc("Сетка интервалов дня встречи; 404 — встречи нет или она отменена (не в продаже)")
  slots(@path id: string): Slot[] | ApiError<404>;

  @route("/{id}/book")
  @post
  @doc("Забронировать интервал; 409 — занят (включая другие встречи организатора), 422 — встреча отменена. При одновременных запросах на пересекающиеся интервалы одного организатора ровно одна запись succeeds, остальные получают 409 (гарантия непересечения)")
  book(@path id: string, @body body: BookRequest):
    | {
        @statusCode code: 201;
        @body booking: Booking;
      }
    | ApiError<404>
    | ApiError<409>
    | ApiError<422>;
}

// --- /api/preview: превью сетки до сохранения встречи (Сценарий 1) ---

@route("/api/preview")
interface PreviewApi {
  @route("/availability")
  @get
  @doc("Окно доступности на 14 дней для создаваемой встречи; шаг стартов необязателен (без него = длительность)")
  availability(
    @query duration: int32,

    @query
    @doc("Шаг стартов: целый; нечисловое значение — 400, вне диапазона 1..duration — 422 (как при создании)")
    step?: int32,

    @query owner?: string,

    @query
    @doc("IANA-зона-фолбэк для расчёта до первого сохранения: переданная > сохранённый профиль > UTC")
    timezone?: string,
  ): DayAvailability[] | ApiError<400> | ApiError<422>;

  @route("/slots")
  @get
  @doc("Сетка интервалов конкретного дня для создаваемой встречи; шаг стартов необязателен (без него = длительность)")
  slots(
    @query owner?: string,
    @query date: plainDate,
    @query duration: int32,

    @query
    @doc("Шаг стартов: целый; нечисловое значение — 400, вне диапазона 1..duration — 422 (как при создании)")
    step?: int32,

    @query
    @doc("IANA-зона-фолбэк для расчёта до первого сохранения: переданная > сохранённый профиль > UTC")
    timezone?: string,
  ): Slot[] | ApiError<400> | ApiError<422>;
}

// --- /api/bookings: «Мои записи» посетителя ---

@route("/api/bookings")
interface BookingsApi {
  @get
  @doc("Все записи пользователя по email; прошедшие помечены past=true")
  list(@query email: string): BookingView[] | ApiError<400>;

  @route("/{id}")
  @delete
  @doc("Отмена Записи Посетителем. Порядок проверок: 404 — нет такой Записи; email сверяется до прочих состояний — чужой email отклоняется (409), в том числе для уже отменённой Записи; повторная отмена тем же email — успешный no-op (204); 422 — Запись уже завершилась.")
  cancel(@path id: string, @query email: string):
    | {
        @statusCode code: 204;
      }
    | ApiError<404>
    | ApiError<409>
    | ApiError<422>;
}

// --- /api/health: health check для оркестраторов и хостинга (Docker/Render) ---

model HealthResponse {
  status: string;
}

@route("/api/health")
interface HealthApi {
  @get
  @doc("Ливенс-проб: 200 { status: ok }")
  health(): {
    @statusCode code: 200;
    @body body: HealthResponse;
  };
}
