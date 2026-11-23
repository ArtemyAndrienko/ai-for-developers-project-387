import { describe, expect, it } from "vitest";
import type { BookingView, Slot, SlotStatus } from "./types";
import {
  addDays,
  buildWindow,
  cancelSourceLabel,
  emailLike,
  formatVisitorTime,
  groupBookings,
  hmToMin,
  isCancelledOrPast,
  isoDate,
  nearestFreeStarts,
  slotLabel,
  visitorClashes,
  weekStartMonday,
} from "./utils";

describe("даты", () => {
  it("addDays переносит через границу месяца и года", () => {
    expect(addDays("2026-11-16", 1)).toBe("2026-11-17");
    expect(addDays("2026-11-30", 1)).toBe("2026-12-01");
    expect(addDays("2026-12-31", 1)).toBe("2027-01-01");
    expect(addDays("2026-11-16", -1)).toBe("2026-11-15");
  });

  it("isoDate форматирует локальную дату", () => {
    expect(isoDate(new Date(2026, 10, 6))).toBe("2026-11-06");
  });

  it("weekStartMonday даёт понедельник", () => {
    // 16.11.2026 — понедельник
    expect(weekStartMonday("2026-11-16")).toBe("2026-11-16");
    expect(weekStartMonday("2026-11-22")).toBe("2026-11-16"); // воскресенье
    expect(weekStartMonday("2026-11-23")).toBe("2026-11-23"); // понедельник
  });

  it("окно доступности — 14 дней от сегодня включительно", () => {
    const w = buildWindow("2026-11-16");
    expect(w).toHaveLength(14);
    expect(w[0]).toBe("2026-11-16");
    expect(w[13]).toBe("2026-11-29");
  });
});

describe("интервалы", () => {
  it("hmToMin переводит HH:MM в минуты", () => {
    expect(hmToMin("09:00")).toBe(540);
    expect(hmToMin("17:30")).toBe(1050);
  });

  it("метки статусов на русском, как в референсе", () => {
    expect(slotLabel("free")).toBe("Свободно");
    expect(slotLabel("taken")).toBe("Занято");
    expect(slotLabel("past")).toBe("Прошло");
  });

  it("метка «Мало времени» упразднена вместе с хвостом (ADR-0003)", () => {
    // tail больше нет в статусной сетке API; словарь не должен его подписывать.
    expect(slotLabel("tail")).toBe("tail");
  });
});

describe("предикат «не предстоящая»", () => {
  const b = (over: Record<string, unknown>) =>
    ({ past: false, state: "active", ...over }) as BookingView;

  it("предстоящая активная Запись — не прошедшая и не отменённая", () => {
    expect(isCancelledOrPast(b({}))).toBe(false);
  });

  it("завершившаяся Запись — true", () => {
    expect(isCancelledOrPast(b({ past: true }))).toBe(true);
  });

  it("отменённая предстоящая Запись — true (группа «Прошедшие», без кнопки)", () => {
    expect(isCancelledOrPast(b({ state: "cancelled" }))).toBe(true);
  });
});

describe("группировка записей (Сценарий 2)", () => {
  it("прошедшие отделяются от предстоящих", () => {
    const { upcoming, past } = groupBookings([
      { past: false },
      { past: true },
      { past: false },
    ]);
    expect(upcoming).toHaveLength(2);
    expect(past).toHaveLength(1);
  });

  it("пустой список — пустые группы (если встреч нет, список пустой)", () => {
    expect(groupBookings([])).toEqual({ upcoming: [], past: [] });
  });
});

describe("мягкий конфликт посетителя (GLOSSARY: Конфликт)", () => {
  const other = (over: Record<string, unknown>) =>
    ({
      past: false,
      state: "active",
      meetingId: "m-9",
      date: "2026-11-20",
      start: "10:00",
      end: "10:30",
      ...over,
    }) as BookingView;

  it("пересекающаяся будущая запись в другой встрече — конфликт", () => {
    const clashes = visitorClashes([other({ start: "10:15", end: "11:00" })], "m-1", "2026-11-20", "10:00", "10:30");
    expect(clashes).toHaveLength(1);
  });

  it("касание границ (конец = начало) — не конфликт", () => {
    expect(visitorClashes([other({ start: "09:30", end: "10:00" })], "m-1", "2026-11-20", "10:00", "10:30")).toHaveLength(0);
  });

  it("та же встреча, другой день и прошедшая запись — не конфликт", () => {
    expect(visitorClashes([other({ meetingId: "m-1" })], "m-1", "2026-11-20", "10:00", "10:30")).toHaveLength(0);
    expect(visitorClashes([other({ date: "2026-11-21" })], "m-1", "2026-11-20", "10:00", "10:30")).toHaveLength(0);
    expect(visitorClashes([other({ past: true })], "m-1", "2026-11-20", "10:00", "10:30")).toHaveLength(0);
  });

  it("отменённая запись не конфликтует — её Интервал свободен", () => {
    const clashes = visitorClashes([other({ state: "cancelled" })], "m-1", "2026-11-20", "10:00", "10:30");
    expect(clashes).toHaveLength(0);
  });
});

describe("источник Отмены (GLOSSARY: Отмена Встречи)", () => {
  const booking = (over: Record<string, unknown>) =>
    ({ state: "active", meetingState: "active", ...over }) as BookingView;

  it("активная Запись не имеет источника отмены", () => {
    expect(cancelSourceLabel(booking({}))).toBeNull();
  });

  it("отменённая Запись активной Встречи — отменено посетителем", () => {
    expect(cancelSourceLabel(booking({ state: "cancelled", meetingState: "active" }))).toBe(
      "отменено посетителем",
    );
  });

  it("Запись отменённой Встречи — отменено организатором", () => {
    expect(cancelSourceLabel(booking({ state: "cancelled", meetingState: "cancelled" }))).toBe(
      "отменено организатором",
    );
  });
});

describe("emailLike", () => {
  it("принимает корректные и отвергает мусор", () => {
    expect(emailLike("a@b.io")).toBe(true);
    expect(emailLike("boss@x.io")).toBe(true);
    expect(emailLike("boss@x")).toBe(false); // требует доменной точки — мягче сервера не надо
    expect(emailLike("nope")).toBe(false);
    expect(emailLike("")).toBe(false);
  });
});

describe("nearestFreeStarts (жёсткий конфликт: ближайшие свободные старты)", () => {
  const slot = (start: string, status: SlotStatus): Slot => ({ start, end: start, status });
  const grid: Slot[] = [
    slot("09:00", "past"),
    slot("09:30", "free"),
    slot("10:00", "taken"),
    slot("10:30", "free"),
    slot("11:00", "free"),
    slot("11:30", "free"),
    slot("12:00", "past"),
  ];

  it("даёт первые свободные старты, начиная с попытки", () => {
    expect(nearestFreeStarts(grid, "10:00").map((s) => s.start)).toEqual(["10:30", "11:00", "11:30"]);
  });

  it("включает саму попытку, если она свободна", () => {
    expect(nearestFreeStarts(grid, "09:30").map((s) => s.start)).toEqual(["09:30", "10:30", "11:00"]);
  });

  it("исключает прошлые и занятые статусами — пустой результат после последних слотов", () => {
    expect(nearestFreeStarts(grid, "13:00")).toEqual([]);
    // последний интервал сетки «прошёл» — в баннер не попадает.
    expect(nearestFreeStarts(grid, "12:00")).toEqual([]);
    expect(nearestFreeStarts([], "10:00")).toEqual([]);
    expect(nearestFreeStarts([slot("09:00", "taken"), slot("09:30", "past")], "09:00")).toEqual([]);
  });

  it("уважает limit", () => {
    expect(nearestFreeStarts(grid, "09:30", 2).map((s) => s.start)).toEqual(["09:30", "10:30"]);
    expect(nearestFreeStarts(grid, "09:30", 1).map((s) => s.start)).toEqual(["09:30"]);
  });
});

describe("formatVisitorTime — отображение времени Посетителю (билет #13)", () => {
  it("разные зоны: часы организатора с подписью пояса + локальное время в скобках", () => {
    // Организатор из Колкаты (UTC+5:30), посетитель из Берлина: 25.10.2026
    // DST в Европе закончился, ноябрьская дата — зимний UTC+1, смещение 4:30.
    const r = formatVisitorTime("10:00", "10:30", "Asia/Kolkata", "Europe/Berlin", "2026-11-15");
    expect(r.sameZone).toBe(false);
    expect(r.primary).toBe("10:00–10:30 GMT+5:30");
    expect(r.secondary).toBe(" (05:30–06:00 у вас)");
  });

  it("одинаковые зоны: чистый вывод без подписей и скобок", () => {
    const r = formatVisitorTime("10:00", "10:30", "Europe/Berlin", "Europe/Berlin", "2026-11-15");
    expect(r.sameZone).toBe(true);
    expect(r.primary).toBe("10:00–10:30");
    expect(r.secondary).toBe("");
  });

  it("Пояс просмотра совпадает с поясом Организатора (не с браузером): чисто (билет #6)", () => {
    // Экран передаёт выбранную Посетителем зону, а не зону браузера:
    // нормализованное совпадение даёт вывод без вторых времени.
    const r = formatVisitorTime("10:00", "10:30", "europe/moscow", "Europe/Moscow", "2026-11-15");
    expect(r.sameZone).toBe(true);
    expect(r.primary).toBe("10:00–10:30");
    expect(r.secondary).toBe("");
  });

  it("перенос через полночь у посетителя, но календарный день тот же: без маркера", () => {
    // 00:30–01:00 MSK (UTC+3) 15.11 = 21:30–22:00 UTC 14.11 = 03:00–03:30
    // Колкаты 15.11: в secondary только HH:MM, календарная дата остаётся днём организатора.
    const r = formatVisitorTime("00:30", "01:00", "Europe/Moscow", "Asia/Kolkata", "2026-11-15");
    expect(r.sameZone).toBe(false);
    expect(r.primary).toBe("00:30–01:00 GMT+3");
    expect(r.secondary).toBe(" (03:00–03:30 у вас)");
  });

  it("перенос суток назад: маркер «−1д » перед вторым временем (билет #5)", () => {
    // 00:30–01:00 MSK 15.11 = 21:30 UTC 14.11 = 16:30–17:00 Нью-Йорка (EST) 14.11.
    const r = formatVisitorTime("00:30", "01:00", "Europe/Moscow", "America/New_York", "2026-11-15");
    expect(r.primary).toBe("00:30–01:00 GMT+3");
    expect(r.secondary).toBe(" (−1д 16:30–17:00 у вас)");
  });

  it("перенос суток вперёд: маркер «+1д » перед вторым временем (билет #5)", () => {
    // 19:00–19:30 Нью-Йорка (EST) 15.11 = 00:00 UTC 16.11 = 05:30–06:00 Колкаты 16.11.
    const r = formatVisitorTime("19:00", "19:30", "America/New_York", "Asia/Kolkata", "2026-11-15");
    expect(r.primary).toBe("19:00–19:30 GMT-5");
    expect(r.secondary).toBe(" (+1д 05:30–06:00 у вас)");
  });

  it("зона посетителя неизвестна (фолбэк Intl): только часы организатора с подписью", () => {
    const r = formatVisitorTime("10:00", "10:30", "Asia/Kolkata", "", "2026-11-15");
    expect(r.sameZone).toBe(false);
    expect(r.primary).toBe("10:00–10:30 GMT+5:30");
    expect(r.secondary).toBe("");
  });
});
