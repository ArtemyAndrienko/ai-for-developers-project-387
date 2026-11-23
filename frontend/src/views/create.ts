// Сценарий 1: организатор создаёт встречу. Поля: название, описание, email
// организатора, продолжительность, часовой пояс (IANA, профиль организатора —
// ADR-0004) и дата (встреча назначается на конкретный день). Конкретный
// интервал не выбирается — список интервалов показывает превью доступности.
// Границы рабочего дня берутся для пары (email, дата) и сохраняются только
// для выбранного дня; сохранение границ обновляет превью НА МЕСТЕ, без
// перезагрузки/сброса формы создания встречи.
// После сохранения — список всех встреч организатора (пустой, если их нет).

import { api } from "../api";
import { buildCalendar, renderCalendar } from "../calendar";
import {
  emailLike,
  OWNER_EMAIL_KEY,
  OWNER_NAME_KEY,
  ruLongDate,
  slotLabel,
  todayIso,
} from "../utils";
import { DEFAULT_ORGANIZER_ZONE, zoneCatalog, zoneOffsetMinutes, zoneOptionLabel } from "../zones";
import type { DayAvailability, Meeting, Slot } from "../types";

interface CreateState {
  name: string;
  description: string;
  owner: string;
  ownerName: string;
  timezone: string;
  duration: number;
  step: number | null; // Шаг стартов; null — «как длительность» (ADR-0003)
  date: string | null; // день, на который назначается встреча
  weekOffset: number;
  availability: DayAvailability[];
  slots: Slot[];
  meetings: Meeting[];
  hoursNote: string;
}

// ADR-0002: канонический набор длительностей («тип события») — пресеты UI;
// сервер по-прежнему принимает любой целый 1..1440.
const CANONICAL_DURATIONS = [15, 30, 45, 60, 90, 120, 150, 180];

// Пресеты шага стартов — только UI-подсказки (ADR-0002 для шага — то же
// правило); сервер валидирует целый 1..длительности.
const STEP_PRESETS = [15, 30, 45, 60, 90];

// Доступные пресеты шага: не больше выбранной длительности.
const stepPresetsFor = (duration: number) => STEP_PRESETS.filter((s) => s <= duration);

// Опции селектора пояса: полный IANA-справочник, отсортированный по факту
// на момент отрисовки (DST учтён), подписи с смещением UTC (билет #4).
function tzOptionsHtml(selected: string): string {
  const now = Date.now();
  return zoneCatalog(now)
    .map(
      (z) =>
        `<option value="${z}"${z === selected ? " selected" : ""}>${zoneOptionLabel(z, zoneOffsetMinutes(z, now))}</option>`,
    )
    .join("");
}

export async function renderCreate(app: HTMLElement): Promise<void> {
  const state: CreateState = {
    name: "",
    description: "",
    owner: localStorage.getItem(OWNER_EMAIL_KEY) ?? "",
    ownerName: localStorage.getItem(OWNER_NAME_KEY) ?? "",
    // Дефолт поля «Часовой пояс» — Europe/Moscow (билет #4): не зона браузера,
    // которая не обязана быть рабочей зоной организатора; опечатку ловит уже
    // сам селектор, выбирающий из IANA-справочника.
    timezone: DEFAULT_ORGANIZER_ZONE,
    duration: 30,
    step: null,
    date: null,
    weekOffset: 0,
    availability: [],
    slots: [],
    meetings: [],
    hoursNote: "",
  };

  state.meetings = await api.meetings(state.owner || undefined);
  state.availability = await api.previewAvailability(state.duration, state.owner, undefined, state.timezone);
  // Каркас рисуется один раз; дальше обновляем только контейнеры превью
  // (#cal, #slots, #hours) — поля формы не пересоздаются (Сценарий 1:
  // сохранение границ дня не должно «перезагружать» страницу создания).
  app.innerHTML = `
    <div class="cols two">
      <div class="card">
        <h3>Новая встреча</h3>
        <div id="form-error"></div>
        <label>Название <input id="name" placeholder="Обязательно" /></label>
        <label>Описание <textarea id="desc" rows="2"></textarea></label>
        <label>Email организатора <input id="owner" type="email" placeholder="boss@example.com" />
          <small class="muted">по нему учитывается занятость интервалов во всех ваших встречах</small></label>
        <label>Имя организатора <input id="owner-name" placeholder="Необязательно, видно посетителям" /></label>
        <label>Часовой пояс
          <select id="tz">${tzOptionsHtml(state.timezone)}</select>
          <small class="muted">IANA-зона: рабочий день, даты и «уже прошло» считаются по вашим часам</small></label>
        <label>Продолжительность
          <select id="dur">
            ${CANONICAL_DURATIONS.map(
              (d) => `<option value="${d}"${d === state.duration ? " selected" : ""}>${d} мин</option>`,
            ).join("")}
          </select>
        </label>
        <label>Шаг стартов
          <select id="step">
            <option value="">как длительность</option>
            ${stepPresetsFor(state.duration).map(
              (s) => `<option value="${s}"${s === state.step ? " selected" : ""}>${s} мин</option>`,
            ).join("")}
          </select>
          <small class="muted">расстояние между доступными началами; старты предлагаются, пока полная длительность влезает в день</small>
        </label>
        <div id="hours"></div>
        <button class="btn primary block" id="save" disabled>Сохранить встречу</button>
      </div>

      <div>
        <div class="card" id="cal"></div>
        <div class="card" style="margin-top:16px">
          <h3>Превью интервалов</h3>
          <div id="slots"></div>
        </div>
      </div>
    </div>

    <h2 class="section-title" id="meetings-title"></h2>
    <div id="meetings"></div>
  `;

  const $ = <T extends HTMLElement>(sel: string) => app.querySelector(sel) as T;
  const nameInput = $<HTMLInputElement>("#name");
  const descInput = $<HTMLTextAreaElement>("#desc");
  const ownerInput = $<HTMLInputElement>("#owner");
  const ownerNameInput = $<HTMLInputElement>("#owner-name");
  const tzSelect = $<HTMLSelectElement>("#tz");
  const durSelect = $<HTMLSelectElement>("#dur");
  const stepSelect = $<HTMLSelectElement>("#step");
  const saveBtn = $<HTMLButtonElement>("#save");
  const calEl = $<HTMLDivElement>("#cal");
  const slotsEl = $<HTMLDivElement>("#slots");
  const hoursEl = $<HTMLDivElement>("#hours");
  const errEl = $<HTMLDivElement>("#form-error");
  ownerInput.value = state.owner;
  ownerNameInput.value = state.ownerName;

  const updateSaveBtn = () => {
    saveBtn.disabled = !(state.name.trim() && emailLike(state.owner) && state.date);
  };

  const setFormError = (msg: string) => {
    errEl.innerHTML = msg ? `<div class="error">${msg}</div>` : "";
  };

  const renderSlots = () => {
    if (!state.date) {
      slotsEl.innerHTML = `<div class="hint">Выберите дату встречи в календаре.</div>`;
      return;
    }
    const stepText = state.step === null ? `${state.duration} мин (как длительность)` : `${state.step} мин`;
    slotsEl.innerHTML = `
      <p class="muted">${ruLongDate(state.date)} · шаг стартов ${stepText}</p>
      <ul class="slots preview">
        ${state.slots
          .map(
            (s) => `<li class="slot${s.status === "free" ? "" : " off"}">
              <span>${s.start} - ${s.end}</span><b>${slotLabel(s.status)}</b></li>`,
          )
          .join("")}
      </ul>`;
  };

  const renderCal = () => {
    const freeByDate = new Map(state.availability.map((d) => [d.date, d.free]));
    renderCalendar(
      calEl,
      buildCalendar(todayIso(), freeByDate, state.weekOffset),
      state.date,
      async (date) => {
        state.date = date;
        state.hoursNote = "";
        await Promise.all([refreshPreview(), loadHours()]);
        updateSaveBtn();
      },
      (delta) => {
        state.weekOffset += delta;
        renderCal();
      },
    );
  };

  // Границы только на выбранный день организатора; без даты/email — дефолт.
  const renderHours = (workStart: string, workEnd: string) => {
    hoursEl.innerHTML = `
      <h4>Рабочий день${state.date ? ` — ${ruLongDate(state.date)}` : ""}</h4>
      ${state.hoursNote ? `<p class="muted">${state.hoursNote}</p>` : ""}
      <div class="row">
        <label>С <input id="ws" value="${workStart}" /></label>
        <label>До <input id="we" value="${workEnd}" /></label>
        <button class="btn ghost" id="save-hours" ${state.date && emailLike(state.owner) ? "" : "disabled"}>
          Сохранить границы
        </button>
      </div>`;
    $<HTMLButtonElement>("#save-hours").addEventListener("click", saveHours);
  };

  const loadHours = async () => {
    if (!state.date || !emailLike(state.owner)) {
      renderHours("09:00", "18:00");
      return;
    }
    try {
      const h = await api.workHours(state.owner, state.date);
      renderHours(h.workStart, h.workEnd);
    } catch {
      renderHours("09:00", "18:00");
    }
  };

  // Сохранение границ: PUT + обновление превью и сетки. Поля формы не
  // трогаем — страница создания не «перезагружается».
  const saveHours = async () => {
    if (!state.date) return;
    const ws = ($<HTMLInputElement>("#ws")).value;
    const we = ($<HTMLInputElement>("#we")).value;
    try {
      await api.setWorkHours(state.owner, state.date, ws, we);
      state.hoursNote = `Границы сохранены только для ${ruLongDate(state.date)}.`;
      setFormError("");
      await refreshPreview();
      renderHours(ws, we);
      renderSlots();
    } catch (e) {
      setFormError(e instanceof Error ? e.message : "Не удалось сохранить границы");
    }
  };

  // Превью считается в зоне формы (переданная > сохранённый профиль > UTC):
  // до первого сохранения «сегодня» и «прошло» идут по часам поля
  // «Часовой пояс», а не локации сервера — иначе у полуночи превью
  // разъезжается с сохранённой встречей. Контейнеры превью (#cal/#slots)
  // перерисовываются, поля формы не трогаются.
  const refreshPreview = async () => {
    const step = state.step ?? undefined;
    const tz = state.timezone.trim();
    state.availability = await api.previewAvailability(state.duration, state.owner, step, tz);
    state.slots = state.date
      ? await api.previewSlots(state.owner, state.date, state.duration, step, tz)
      : [];
    renderCal();
    renderSlots();
  };

  const renderMeetings = () => {
    $("h2#meetings-title").textContent = state.owner ? "Ваши встречи" : "Все встречи";
    const list = $("div#meetings");
    if (state.meetings.length === 0) {
      list.innerHTML = `<div class="empty-state">Встреч пока нет — создайте первую выше.</div>`;
      return;
    }
    list.innerHTML = `<ul class="meetings">
      ${state.meetings
        .map(
          (m) => `
        <li class="card meeting-row">
          <div><strong>${m.name}</strong> · ${ruLongDate(m.date)} · ${m.durationMin} мин
            <div class="muted">${m.description ?? ""}</div></div>
          <button class="btn danger" data-del="${m.id}">Отменить встречу</button>
        </li>`,
        )
        .join("")}
    </ul>`;
    list.querySelectorAll<HTMLElement>("[data-del]").forEach((btn) =>
      btn.addEventListener("click", async () => {
        // Мягкая Отмена Встречи: сервер помечает её cancelled, и она
        // исчезает из списка (фильтр на стороне Store).
        await api.deleteMeeting(btn.dataset.del!);
        state.meetings = await api.meetings(state.owner || undefined);
        await refreshPreview(); // занятость организатора пересчитана
        renderMeetings();
      }),
    );
  };

  // --- привязка полей формы (значения живут в state, но DOM не пересоздаём) ---
  nameInput.addEventListener("input", () => {
    state.name = nameInput.value;
    updateSaveBtn();
  });
  descInput.addEventListener("input", () => {
    state.description = descInput.value;
  });
  ownerInput.addEventListener("input", () => {
    state.owner = ownerInput.value;
    updateSaveBtn();
  });
  ownerNameInput.addEventListener("input", () => {
    state.ownerName = ownerNameInput.value;
  });
  // Смена пояса пересчитывает превью на месте: сетка и окно доступности
  // живут в часах указанной зоны ещё до сохранения профиля (US-6).
  tzSelect.addEventListener("change", async () => {
    state.timezone = tzSelect.value.trim();
    await refreshPreview();
  });
  ownerInput.addEventListener("change", async () => {
    state.owner = ownerInput.value.trim();
    await Promise.all([refreshPreview(), loadHours()]);
    state.meetings = await api.meetings(state.owner || undefined);
    renderMeetings();
    updateSaveBtn();
  });
  durSelect.addEventListener("change", async () => {
    state.duration = Number(durSelect.value);
    // Шаг не может быть больше длительности: устаревший выбор сбрасываем
    // в «как длительность» и перестраиваем пресеты.
    if (state.step !== null && state.step > state.duration) {
      state.step = null;
    }
    stepSelect.innerHTML = `<option value="">как длительность</option>${stepPresetsFor(state.duration)
      .map((s) => `<option value="${s}"${s === state.step ? " selected" : ""}>${s} мин</option>`)
      .join("")}`;
    stepSelect.value = state.step === null ? "" : String(state.step);
    await refreshPreview();
  });
  stepSelect.addEventListener("change", async () => {
    state.step = stepSelect.value === "" ? null : Number(stepSelect.value);
    await refreshPreview();
  });

  saveBtn.addEventListener("click", async () => {
    if (!state.date) return;
    try {
      await api.createMeeting({
        name: state.name.trim(),
        description: state.description.trim(),
        durationMin: state.duration,
        startStepMin: state.step ?? undefined,
        ownerEmail: state.owner.trim(),
        ownerName: state.ownerName.trim(),
        date: state.date,
        timezone: state.timezone.trim() || undefined,
      });
      localStorage.setItem(OWNER_EMAIL_KEY, state.owner.trim());
      localStorage.setItem(OWNER_NAME_KEY, state.ownerName.trim());
      setFormError("");
      state.name = "";
      state.description = "";
      nameInput.value = "";
      descInput.value = "";
      state.meetings = await api.meetings(state.owner || undefined);
      await refreshPreview();
      renderMeetings();
    } catch (e) {
      setFormError(e instanceof Error ? e.message : "Не удалось создать встречу");
    }
  });

  renderCal();
  renderSlots();
  renderHours("09:00", "18:00");
  renderMeetings();
  updateSaveBtn();
}
