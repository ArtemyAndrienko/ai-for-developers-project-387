// Сеточный календарь как в референсе: недели Пн–Вс, листание по неделям
// ← / →, бейдж «N св.» в ячейке, вне окна и прошедшие дни — серые.

import { addDays, buildWindow, ruMonthYear, weekStartMonday } from "./utils";

export interface CalCell {
  date: string; // ISO
  day: number; // число в подписи ячейки
  inWindow: boolean;
  free: number | null; // null — вне окна
}

export interface WeekModel {
  label: string; // «ноябрь 2026 г.» первой ячейки
  cells: CalCell[]; // 7 ячеек
}

export interface CalendarModel {
  weeks: WeekModel[];
  canPrev: boolean;
  canNext: boolean;
}

// weekOffset — на сколько недель вперёд от текущей листаем (0..2 для окна 14 дней).
export function buildCalendar(
  today: string,
  freeByDate: Map<string, number>,
  weekOffset: number,
  days = 14,
): CalendarModel {
  const gridStart = addDays(weekStartMonday(today), 7 * weekOffset);
  const windowSet = new Set(buildWindow(today, days));
  // Сколько недель сетки нужно, чтобы покрыть окно с этого смещения.
  const offsetInWeek = (new Date(`${today}T00:00:00`).getDay() + 6) % 7;
  const totalWeeks = Math.min(
    4,
    Math.ceil((days - 7 * weekOffset + offsetInWeek) / 7),
  );

  const weeks: WeekModel[] = [];
  for (let w = 0; w < Math.max(1, totalWeeks); w++) {
    const cells: CalCell[] = [];
    for (let i = 0; i < 7; i++) {
      const date = addDays(gridStart, 7 * w + i);
      const inWindow = windowSet.has(date);
      cells.push({ date, day: Number(date.slice(8)), inWindow, free: inWindow ? freeByDate.get(date) ?? 0 : null });
    }
    weeks.push({ label: ruMonthYear(cells[0].date), cells });
  }
  const maxOffset = Math.ceil((days + offsetInWeek) / 7) - 1;
  return { weeks, canPrev: weekOffset > 0, canNext: weekOffset < maxOffset };
}

export function renderCalendar(
  host: HTMLElement,
  model: CalendarModel,
  selectedDate: string | null,
  onSelect: (date: string) => void,
  onNav: (delta: number) => void,
): void {
  const weekdays = ["Пн", "Вт", "Ср", "Чт", "Пт", "Сб", "Вс"];
  host.innerHTML = `
    <div class="cal-head">
      <h3>Календарь</h3>
      <div class="cal-nav">
        <button class="nav-btn" data-nav="-1" ${model.canPrev ? "" : "disabled"}>←</button>
        <button class="nav-btn" data-nav="1" ${model.canNext ? "" : "disabled"}>→</button>
      </div>
    </div>
    ${model.weeks
      .map(
        (week) => `
      <div class="cal-week-label">${week.label}</div>
      <div class="cal-grid">
        ${weekdays.map((w) => `<div class="cal-dow">${w}</div>`).join("")}
        ${week.cells
          .map((c) => {
            const cls = c.inWindow ? (c.free && c.free > 0 ? "cal-day" : "cal-day empty") : "cal-day off";
            const badge = c.inWindow && c.free && c.free > 0 ? `<small>${c.free} св.</small>` : "";
            const sel = c.date === selectedDate ? " selected" : "";
            return `<button class="${cls}${sel}" data-date="${c.date}" ${c.inWindow && c.free ? "" : "disabled"}>
              <span>${c.day}</span>${badge}</button>`;
          })
          .join("")}
      </div>`,
      )
      .join("")}
  `;
  host.querySelectorAll<HTMLElement>("[data-date]").forEach((btn) => {
    btn.addEventListener("click", () => onSelect(btn.dataset.date as string));
  });
  host.querySelectorAll<HTMLElement>("[data-nav]").forEach((btn) => {
    btn.addEventListener("click", () => onNav(Number(btn.dataset.nav)));
  });
}
