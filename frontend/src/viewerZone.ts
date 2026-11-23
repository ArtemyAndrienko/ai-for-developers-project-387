// Общая lower-ишка Пояса просмотра для экранов Посетителя (билеты #6/#7):
// один ключ localStorage calendar.viewerZone на «Запись на звонок» и
// «Мои записи» — выбор синхронен между экранами и переживает перезагрузку.
// Разрешение значения (сохранённая → браузерная → "") — чистая функция из
// zones.ts, DOM-обвязка здесь unit-тестам не подлежит (ADR-0005).

import { browserZone } from "./utils";
import {
  isValidIanaZone,
  resolveViewerZone,
  zoneCatalog,
  zoneOffsetMinutes,
  zoneOptionLabel,
  VIEWER_ZONE_KEY,
} from "./zones";

export function loadViewerZone(): string {
  return resolveViewerZone(localStorage.getItem(VIEWER_ZONE_KEY) ?? "", browserZone());
}

// Селектор внизу экрана: подпись + полный IANA-справочник (тот же, что на
// создании, билет #4). Значение display-only: вердикты, даты и Окно
// доступности остаются в поясе Организатора (ADR-0004).
export function viewerZoneControlHtml(zone: string): string {
  const now = Date.now();
  const catalog = zoneCatalog(now);
  const current = isValidIanaZone(zone) ? zone : catalog[0] ?? "";
  return `
    <div class="card viewer-zone">
      <label>Пояс просмотра
        <select id="viewer-tz">${catalog
          .map(
            (z) =>
              `<option value="${z}"${z === current ? " selected" : ""}>${zoneOptionLabel(
                z,
                zoneOffsetMinutes(z, now),
              )}</option>`,
          )
          .join("")}</select>
      </label>
      <small class="muted">в него конвертируется «второе» время Интервалов; часы Организатора и даты не меняются</small>
    </div>`;
}

// Привязка селектора: выбор пишется в общий ключ и перерисовывает экран.
export function wireViewerZone(host: HTMLElement, onChange: (zone: string) => void): void {
  const select = host.querySelector<HTMLSelectElement>("#viewer-tz");
  if (!select) return;
  select.addEventListener("change", () => {
    const zone = select.value;
    localStorage.setItem(VIEWER_ZONE_KEY, zone);
    onChange(zone);
  });
}
