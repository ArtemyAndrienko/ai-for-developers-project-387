// Чистые функции без обращения к API — покрываются vitest.

export const WINDOW_DAYS = 14;
export const LAST_EMAIL_KEY = "calendar.email";
export const OWNER_EMAIL_KEY = "calendar.ownerEmail";
export const OWNER_NAME_KEY = "calendar.ownerName";
export const HIDE_PAST_KEY = "calendar.hidePast";

export function isoDate(d: Date): string {
  const y = d.getFullYear();
  const m = String(d.getMonth() + 1).padStart(2, "0");
  const day = String(d.getDate()).padStart(2, "0");
  return `${y}-${m}-${day}`;
}

export function todayIso(now: Date = new Date()): string {
  return isoDate(now);
}

export function addDays(iso: string, n: number): string {
  const d = new Date(`${iso}T00:00:00`);
  d.setDate(d.getDate() + n);
  return isoDate(d);
}

// Понедельник недели, к которой относится дата (как в референсе: Пн–Вс).
export function weekStartMonday(iso: string): string {
  const d = new Date(`${iso}T00:00:00`);
  const wd = (d.getDay() + 6) % 7; // 0 = понедельник
  return addDays(iso, -wd);
}

// Окно доступности: windowDays дней, начиная с сегодняшнего (Сценарий 1, шаг 2).
export function buildWindow(today: string, days = WINDOW_DAYS): string[] {
  return Array.from({ length: days }, (_, i) => addDays(today, i));
}

export function hmToMin(hm: string): number {
  const [h, m] = hm.split(":").map(Number);
  return h * 60 + m;
}

const slotLabels: Record<string, string> = {
  free: "Свободно",
  taken: "Занято",
  past: "Прошло",
};

export function slotLabel(status: string): string {
  return slotLabels[status] ?? status;
}

export const emailLike = (s: string) => /\S+@\S+\.\S+/.test(s.trim());

import type { BookingView, Slot } from "./types";

// Единый предикат «не предстоящая»: завершившаяся или отменённая Запись —
// группа «Прошедшие», без кнопки отмены (используется и для конфликтов).
export function isCancelledOrPast(b: Pick<BookingView, "past" | "state">): boolean {
  return b.past || b.state === "cancelled";
}

// Мягкий конфликт (GLOSSARY: «Конфликт»): пересечение интервала с будущей
// записью посетителя в ДРУГОЙ встрече. Бронированию не мешает — только
// предупреждаем. Отменённые Записи не пересекаются: их Интервалы свободны.
export function visitorClashes(
  bookings: BookingView[],
  meetingId: string,
  meetingDate: string,
  slotStart: string,
  slotEnd: string,
): BookingView[] {
  const s = hmToMin(slotStart);
  const e = hmToMin(slotEnd);
  return bookings.filter(
    (b) =>
      !isCancelledOrPast(b) &&
      b.meetingId !== meetingId &&
      b.date === meetingDate &&
      hmToMin(b.start) < e &&
      s < hmToMin(b.end),
  );
}

// Жёсткий конфликт (GLOSSARY: «Конфликт» по Организатору): после 409 предлагаем
// ближайшие свободные старты из перезагруженной сетки, начиная с попытки.
// В баннер попадают только свободные интервалы: прошедшие и занятые отсекает
// сам статус, «не раньше сейчас» обеспечивает сетка. Без имён встреч и
// записей: приватность (билет #6).
export function nearestFreeStarts(slots: Slot[], attemptedStart: string, limit = 3): Slot[] {
  return slots.filter((s) => s.status === "free" && s.start >= attemptedStart).slice(0, limit);
}

// Источник терминального «Отменена» выводится из состояний, отдельно не
// хранится (GLOSSARY: «Отмена Встречи»): отменённая Встреча — отмена
// Организатора, иначе Запись отменил Посетитель. Для активной Записи — null.
export function cancelSourceLabel(booking: BookingView): string | null {
  if (booking.state !== "cancelled") return null;
  return booking.meetingState === "cancelled"
    ? "отменено организатором"
    : "отменено посетителем";
}

// Сценарий 2 (билет #13): зона браузера посетителя — IANA-имя или "" при
// отказе Intl. Фолбэк "" означает «неизвестно»: локальное время не показываем.
export function browserZone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone ?? "";
  } catch {
    return "";
  }
}

// «Настенные» минуты зоны для переданного момента (epoch ms): смещение берёт на
// себя Intl по самому instant, а не по «сейчас», — DST-фаза соответствует дате
// вызывающего.
function zoneWallMinutes(zone: string, instantMs: number): number {
  const parts = new Intl.DateTimeFormat("en-US", {
    timeZone: zone,
    hourCycle: "h23",
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  }).formatToParts(new Date(instantMs));
  const value = (type: string) => parts.find((p) => p.type === type)!.value;
  const wall = Date.UTC(
    Number(value("year")),
    Number(value("month")) - 1,
    Number(value("day")),
    Number(value("hour")) % 24,
    Number(value("minute")),
    Number(value("second")),
  );
  return Math.floor(wall / 60000);
}

// Момент (epoch ms), когда в зоне zone настенные часы показывают date HH:MM:
// смещение вычисляется по переданной дате, а не по текущему времени.
// Вторая итерация уточняет смещение на случай DST-перехода в интервале guesses.
function wallToInstant(date: string, hm: string, zone: string): number {
  const [h, m] = hm.split(":").map(Number);
  const guess = Date.parse(`${date}T00:00:00Z`) + (h * 60 + m) * 60000;
  let instant = guess - (zoneWallMinutes(zone, guess) - Math.floor(guess / 60000)) * 60000;
  instant = guess - (zoneWallMinutes(zone, instant) - Math.floor(instant / 60000)) * 60000;
  return instant;
}

// Человекочитаемая подпись пояса организатора на дату (GMT+3/МСК — что даст
// Intl). Якорь — полдень даты, чтобы смещение взять в DST-фазе того дня.
function zoneShortLabel(zone: string, date: string): string {
  try {
    const parts = new Intl.DateTimeFormat("ru-RU", {
      timeZone: zone,
      timeZoneName: "short",
    }).formatToParts(new Date(`${date}T12:00:00Z`));
    return parts.find((p) => p.type === "timeZoneName")?.value ?? "";
  } catch {
    return ""; // неIANA-мусор в зоне — подпись просто отсутствует
  }
}

export interface VisitorTime {
  primary: string; // «10:00–10:30» + подпись пояса организатора (кроме sameZone)
  secondary: string; // « (08:00–10:00 у вас)» — локальные границы посетителя,
  // если зоны различаются; при расхождении календарных дней добавляется
  // маркер «, предыдущий день» / «, следующий день»
  sameZone: boolean;
}

// Отображение времени Посетителю (билет #13, README сценарий 2): первичны часы
// Организатора — HH:MM из сетки уже в его зоне, — с подписью пояса; локальное
// время браузера — в скобках с меткой «у вас» только при различии зон. Даты
// НЕ конвертируются: календарная подпись остаётся днём Организатора, date
// нужен для DST-смещения и для маркера переноса суток в secondary.
export function formatVisitorTime(
  start: string,
  end: string,
  organizerZone: string,
  visitorZone: string,
  date: string,
): VisitorTime {
  const plain = `${start}–${end}`;
  const norm = (z: string) => z.trim().toLowerCase();
  if (organizerZone && norm(organizerZone) === norm(visitorZone)) {
    return { primary: plain, secondary: "", sameZone: true };
  }
  const label = organizerZone ? zoneShortLabel(organizerZone, date) : "";
  let secondary = "";
  try {
    if (organizerZone && visitorZone) {
      const organizerDay = Math.floor(Date.parse(`${date}T00:00:00Z`) / 86_400_000);
      const toLocal = (hm: string) => {
        const mins = zoneWallMinutes(visitorZone, wallToInstant(date, hm, organizerZone));
        const ofDay = ((mins % 1440) + 1440) % 1440; // перенос через полночь — только HH:MM
        return {
          hm: `${String(Math.floor(ofDay / 60)).padStart(2, "0")}:${String(ofDay % 60).padStart(2, "0")}`,
          day: Math.floor(mins / 1440), // календарный день посетителя (отсчёт от epoch)
        };
      };
      const from = toLocal(start);
      const to = toLocal(end);
      // Маркер только при реальном расхождении календарных дней (по старту);
      // сама подпись даты выше не меняется.
      const shift =
        from.day < organizerDay ? ", предыдущий день" : from.day > organizerDay ? ", следующий день" : "";
      secondary = ` (${from.hm}–${to.hm} у вас${shift})`;
    }
  } catch {
    secondary = ""; // неизвестная/битая зона посетителя — без локальной строки
  }
  return { primary: label ? `${plain} ${label}` : plain, secondary, sameZone: false };
}

const ruDate = new Intl.DateTimeFormat("ru-RU", {
  weekday: "long",
  day: "numeric",
  month: "long",
});

export function ruLongDate(iso: string): string {
  return ruDate.format(new Date(`${iso}T00:00:00`));
}

export function ruMonthYear(iso: string): string {
  return new Intl.DateTimeFormat("ru-RU", { month: "long", year: "numeric" }).format(
    new Date(`${iso}T00:00:00`),
  );
}

// Сценарий 2: разделение записей на предстоящие и прошедшие.
export function groupBookings<T extends { past: boolean }>(
  items: T[],
): { upcoming: T[]; past: T[] } {
  return {
    upcoming: items.filter((b) => !b.past),
    past: items.filter((b) => b.past),
  };
}
