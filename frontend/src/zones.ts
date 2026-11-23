// IANA-справочник зон для селекторов (билет #4, спека #3): чистые функции без
// обращения к DOM/API — покрываются vitest. Живой Intl только в тонких
// обёртках, сортировка проверяется на переданном массиве зон.

// Дефолт поля «Часовой пояс» на создании Встречи (спека #3): типовое создание
// не требует трогать селектор; зона браузера — нет, она не обязана быть
// рабочей зоной Организатора.
export const DEFAULT_ORGANIZER_ZONE = "Europe/Moscow";

// Ключ localStorage для Пояса просмотра Посетителя (билет #6, спека #3):
// один ключ на оба экрана посетителя, переживает переходы и перезагрузки.
export const VIEWER_ZONE_KEY = "calendar.viewerZone";

export function isValidIanaZone(zone: string): boolean {
  if (!zone) return false;
  try {
    new Intl.DateTimeFormat("en-US", { timeZone: zone });
    return true;
  } catch {
    return false;
  }
}

// Фактическое смещение зоны от UTC в момент instantMs (мг), учёт DST:
// «настенные» минуты минус epoch-минуты. Зона вне справочника — NaN.
export function zoneOffsetMinutes(zone: string, instantMs: number): number {
  try {
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
    return Math.round(wall / 60000) - Math.floor(instantMs / 60000);
  } catch {
    return NaN;
  }
}

const offsetFormat = (min: number): string => {
  const sign = min < 0 ? "−" : "+";
  const abs = Math.abs(min);
  const h = Math.floor(abs / 60);
  const m = abs % 60;
  return m ? `${sign}${h}:${String(m).padStart(2, "0")}` : `${sign}${h}`;
};

export const zoneOptionLabel = (zone: string, offsetMin: number): string =>
  Number.isNaN(offsetMin) ? zone : `${zone} (UTC${offsetFormat(offsetMin)})`;

// Сортировка справочника (спека #3): по фактическому смещению от UTC в момент
// отрисовки nowMs (DST уже учтён Intl), внутри одного смещения — алфавитно.
// Чистая функция: входной массив не изменяется, зависимость от живого списка
// среды вынесена в supportedIanaZones, тесты гоняют этот слой на фиксированном
// массиве зон.
export function sortZonesByOffset(zones: string[], nowMs: number): string[] {
  const offsets = new Map(zones.map((z) => [z, zoneOffsetMinutes(z, nowMs)]));
  return [...zones].sort((a, b) => {
    const oa = offsets.get(a)!;
    const ob = offsets.get(b)!;
    if (oa !== ob) return oa - ob;
    return a < b ? -1 : a > b ? 1 : 0;
  });
}

// Полный IANA-справочник среды, отсортированный для селектора. Если среда не
// отдаёт supportedValuesOf или список пуст — только DEFAULT_ORGANIZER_ZONE,
// чтобы сабмит создания не ломался (билет #4).
export function zoneCatalog(nowMs: number = Date.now()): string[] {
  let zones: string[];
  try {
    zones = Intl.supportedValuesOf("timeZone");
  } catch {
    zones = [];
  }
  const base = zones.length > 0 ? zones : [DEFAULT_ORGANIZER_ZONE];
  if (!base.includes(DEFAULT_ORGANIZER_ZONE)) base.push(DEFAULT_ORGANIZER_ZONE);
  return sortZonesByOffset(base, nowMs);
}

// Разрешение значения Пояса просмотра (спека #3, билет #6, чистая часть):
// сохранённая валидная зона → она; битая/пустая сохранённая → браузерная;
// обе битые → "" (вторые время молчат).
export function resolveViewerZone(stored: string, browser: string): string {
  if (isValidIanaZone(stored)) return stored;
  return isValidIanaZone(browser) ? browser : "";
}
