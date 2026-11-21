// Сценарий 2 (финал): все записи посетителя по email; пройдена или отменена —
// в «Прошедшие», которые можно скрыть кнопкой «Скрыть неактуальное».
// Предстоящую Запись посетитель может отменить (подтверждение вторым кликом).
// Источник отмены («организатор» / «посетитель») выводится из state/meetingState.

import { api } from "../api";
import {
  browserZone,
  cancelSourceLabel,
  formatVisitorTime,
  groupBookings,
  HIDE_PAST_KEY,
  isCancelledOrPast,
  LAST_EMAIL_KEY,
  ruLongDate,
} from "../utils";
import type { BookingView } from "../types";

export async function renderMine(app: HTMLElement): Promise<void> {
  let hidePast = localStorage.getItem(HIDE_PAST_KEY) === "1";
  let email = localStorage.getItem(LAST_EMAIL_KEY) ?? "";
  let bookings: BookingView[] | null = email ? await fetchBookings(email) : null;
  let error: string | null = null;

  const ui = () => {
    app.innerHTML = `
      <div class="card">
        <div class="row">
          <label>Email для поиска записей
            <input id="email" type="email" placeholder="you@example.com" value="${email}" />
          </label>
          <button class="btn primary" id="find">Найти</button>
        </div>
      </div>
      ${error ? `<div class="error">${error}</div>` : ""}
      ${bookings === null ? `<div class="hint">Введите email, чтобы увидеть свои записи.</div>` : listHtml(bookings, hidePast)}`;

    app.querySelector("#find")!.addEventListener("click", async () => {
      email = (app.querySelector("#email") as HTMLInputElement).value.trim();
      localStorage.setItem(LAST_EMAIL_KEY, email);
      error = null;
      bookings = await fetchBookings(email);
      ui();
    });

    const toggle = app.querySelector("#toggle-past");
    if (toggle) {
      toggle.addEventListener("click", () => {
        hidePast = !hidePast;
        localStorage.setItem(HIDE_PAST_KEY, hidePast ? "1" : "0");
        ui();
      });
    }

    app.querySelectorAll<HTMLButtonElement>("[data-cancel]").forEach((btn) =>
      btn.addEventListener("click", async () => {
        // Подтверждение вторым кликом, без модалки.
        if (btn.dataset.confirm !== "1") {
          btn.textContent = "Точно отменить?";
          btn.dataset.confirm = "1";
          return;
        }
        btn.disabled = true;
        const mail = (app.querySelector("#email") as HTMLInputElement).value.trim();
        try {
          await api.cancelBooking(btn.dataset.cancel!, mail);
          error = null;
          bookings = await fetchBookings(mail);
        } catch (e) {
          error = e instanceof Error ? e.message : "Не удалось отменить запись.";
        }
        ui();
      }),
    );
  };

  ui();
}

async function fetchBookings(email: string): Promise<BookingView[]> {
  if (!email) return [];
  return api.bookings(email);
}

function listHtml(bookings: BookingView[], hidePast: boolean): string {
  // Отменённые Записи — не предстоящие: попадают в «Прошедшие» с бейджем.
  const grouped = bookings.map((b) => ({ ...b, past: isCancelledOrPast(b) }));
  const { upcoming, past } = groupBookings(grouped);
  const organizer = (b: BookingView) =>
    [b.meetingOwnerName, b.meetingOwnerEmail].filter(Boolean).join(" · ");
  const card = (b: BookingView) => {
    const source = cancelSourceLabel(b);
    // Билет #13: то же правило отображения, что на экране записи, — часы
    // Организатора встречи (без профиля по контракту — UTC) с подписью пояса.
    // Решение #13 Q3=a: локальное время в скобках — только у предстоящих;
    // прошедшим и отменённым хватает часов Организатора с подписью.
    const isUpcoming = !isCancelledOrPast(b);
    const zone = b.timezone || "UTC";
    const t = formatVisitorTime(b.start, b.end, zone, browserZone(), b.date);
    return `
    <li class="card booking${b.past ? " off" : ""}">
      <div class="booking-head">
        <strong>${b.meetingName || "Встреча"}</strong>
        ${source ? `<span class="badge">Отменена · ${source}</span>` : ""}
        ${organizer(b) ? `<span class="organizer">Организатор: ${organizer(b)}</span>` : ""}
      </div>
      <div title="${zone}">${ruLongDate(b.date)} · ${t.primary}${isUpcoming ? t.secondary : ""}</div>
      <div class="muted">${b.name}, ${b.email}</div>
      <div class="muted">Создано: ${new Date(b.createdAt).toLocaleString("ru-RU")}</div>
      ${
        isUpcoming
          ? `<button class="btn danger" data-cancel="${b.id}">Отменить</button>`
          : ""
      }
    </li>`;
  };

  return `
    <div class="row between">
      <h2 class="section-title">Предстоящие (${upcoming.length})</h2>
      ${past.length ? `<button class="btn ghost" id="toggle-past">${hidePast ? "Показать неактуальное" : "Скрыть неактуальное"}</button>` : ""}
    </div>
    ${upcoming.length ? `<ul class="booking-list">${upcoming.map(card).join("")}</ul>` : `<div class="empty-state">Предстоящих записей нет.</div>`}
    ${
      past.length && !hidePast
        ? `<h2 class="section-title">Прошедшие (${past.length})</h2>
           <ul class="booking-list">${past.map(card).join("")}</ul>`
        : ""
    }`;
}
