import { describe, expect, it } from "vitest";
import { buildCalendar } from "./calendar";

const free = (pairs: [string, number][]) => new Map(pairs);

describe("buildCalendar", () => {
  // 16.11.2026 — понедельник, окно 14 дней: 16–29.11.2026.
  const today = "2026-11-16";
  const allFree = free(
    Array.from({ length: 14 }, (_, i) => [`2026-11-${String(16 + i).padStart(2, "0")}`, 18] as [string, number]),
  );

  it("первая неделя начинается с понедельника текущей недели", () => {
    const m = buildCalendar(today, allFree, 0);
    expect(m.weeks[0].cells.map((c) => c.date)).toEqual([
      "2026-11-16",
      "2026-11-17",
      "2026-11-18",
      "2026-11-19",
      "2026-11-20",
      "2026-11-21",
      "2026-11-22",
    ]);
  });

  it("последний день окна попадает на вторую неделю", () => {
    const m = buildCalendar(today, allFree, 0);
    const w1 = m.weeks[1].cells.find((c) => c.date === "2026-11-29")!;
    expect(w1.inWindow).toBe(true);
    expect(w1.free).toBe(18);
    // при Monday-today окно 14 дней — ровно две недели сетки
    expect(m.weeks).toHaveLength(2);
  });

  it("дни до начала окна в сетке — вне окна, free=null", () => {
    // today — суббота 21.11: сетка начинается с пн 16.11, дни 16–20 вне окна
    const m = buildCalendar("2026-11-21", new Map([["2026-11-22", 18]]), 0);
    const cells = m.weeks[0].cells;
    expect(cells[0].date).toBe("2026-11-16");
    expect(cells[0].inWindow).toBe(false);
    expect(cells[0].free).toBeNull();
    const sat = cells[5];
    expect(sat.date).toBe("2026-11-21");
    expect(sat.inWindow).toBe(true); // сегодня
    const sun = cells[6];
    expect(sun.date).toBe("2026-11-22");
    expect(sun.free).toBe(18);
  });

  it("день с нулём свободных интервалов остаётся в окне, но не выбирается как «св.»", () => {
    const m = buildCalendar(today, free([["2026-11-16", 0]]), 0);
    const cell = m.weeks[0].cells[0];
    expect(cell.inWindow).toBe(true);
    expect(cell.free).toBe(0);
  });

  it("навигация по неделям ограничена окном", () => {
    const m0 = buildCalendar(today, allFree, 0);
    expect(m0.canPrev).toBe(false);
    const mLast = buildCalendar(today, allFree, 2);
    expect(mLast.canNext).toBe(false);
    expect(mLast.canPrev).toBe(true);
  });

  it("окно через границу месяцев: 25.11 + 14 дней до 08.12", () => {
    const m = buildCalendar("2026-11-25", new Map(), 0);
    const dates = m.weeks.flatMap((w) => w.cells.filter((c) => c.inWindow).map((c) => c.date));
    expect(dates[0]).toBe("2026-11-25");
    expect(dates).toContain("2026-12-01");
    expect(dates[dates.length - 1]).toBe("2026-12-08");
  });
});
