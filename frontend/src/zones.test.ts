import { describe, expect, it, vi } from "vitest";
import {
  DEFAULT_ORGANIZER_ZONE,
  isValidIanaZone,
  resolveViewerZone,
  sortZonesByOffset,
  zoneCatalog,
  zoneOffsetMinutes,
  zoneOptionLabel,
} from "./zones";

const WINTER_MS = Date.UTC(2026, 10, 15, 12); // 2026-11-15T12:00Z — DST в Европе закончился
const SUMMER_MS = Date.UTC(2026, 6, 15, 12); // 2026-07-15T12:00Z — Европа/США в DST

describe("zoneOffsetMinutes — смещение с учётом DST", () => {
  it("зимой/летом берёт фазу DST на переданный момент", () => {
    expect(zoneOffsetMinutes("Europe/Berlin", WINTER_MS)).toBe(60);
    expect(zoneOffsetMinutes("Europe/Berlin", SUMMER_MS)).toBe(120);
    expect(zoneOffsetMinutes("America/New_York", WINTER_MS)).toBe(-300); // EST
    expect(zoneOffsetMinutes("America/New_York", SUMMER_MS)).toBe(-240); // EDT
    expect(zoneOffsetMinutes("Asia/Kolkata", WINTER_MS)).toBe(330);
    expect(zoneOffsetMinutes("Asia/Kolkata", SUMMER_MS)).toBe(330);
    expect(zoneOffsetMinutes("Europe/Moscow", SUMMER_MS)).toBe(180);
  });

  it("битая зона — NaN, а не исключение", () => {
    expect(Number.isNaN(zoneOffsetMinutes("Mars/Olympus", WINTER_MS))).toBe(true);
  });
});

describe("sortZonesByOffset — справочник для селектора (билет #4)", () => {
  it("по смещению от UTC, DST-пары Берлин/Нью-Йорк идут по фактической фазе", () => {
    const zones = ["Asia/Tokyo", "America/New_York", "Europe/Moscow", "Europe/Berlin", "Asia/Kolkata"];
    expect(sortZonesByOffset(zones, WINTER_MS)).toEqual([
      "America/New_York", // −5 (EST)
      "Europe/Berlin", // +1 (CET)
      "Europe/Moscow", // +3
      "Asia/Kolkata", // +5:30
      "Asia/Tokyo", // +9
    ]);
    expect(sortZonesByOffset(zones, SUMMER_MS)).toEqual([
      "America/New_York", // −4 (EDT)
      "Europe/Berlin", // +2 (CEST)
      "Europe/Moscow", // +3
      "Asia/Kolkata", // +5:30
      "Asia/Tokyo", // +9
    ]);
  });

  it("внутри одного смещения — алфавитно", () => {
    const zones = ["Etc/UTC", "Africa/Abidjan", "Europe/Lisbon", "Atlantic/Reykjavik"];
    expect(sortZonesByOffset(zones, WINTER_MS)).toEqual([
      "Africa/Abidjan", // +0
      "Atlantic/Reykjavik", // +0
      "Etc/UTC", // +0
      "Europe/Lisbon", // +0 (WET, переход в конце октября уже прошёл)
    ]);
  });

  it("не изменяет входной массив", () => {
    const zones = ["Asia/Tokyo", "Europe/Berlin"];
    sortZonesByOffset(zones, WINTER_MS);
    expect(zones).toEqual(["Asia/Tokyo", "Europe/Berlin"]);
  });
});

describe("zoneOptionLabel — подпись опции с смещением", () => {
  it("UTC±H:mm, минус — unicode «−»", () => {
    expect(zoneOptionLabel("Europe/Moscow", 180)).toBe("Europe/Moscow (UTC+3)");
    expect(zoneOptionLabel("Asia/Kolkata", 330)).toBe("Asia/Kolkata (UTC+5:30)");
    expect(zoneOptionLabel("America/New_York", -300)).toBe("America/New_York (UTC−5)");
    expect(zoneOptionLabel("Mars/Olympus", NaN)).toBe("Mars/Olympus");
  });
});

describe("zoneCatalog — живой справочник", () => {
  it("содержит Europe/Moscow и отсортирован по смещению, внутри смещения — алфавитно", () => {
    const catalog = zoneCatalog(WINTER_MS);
    expect(catalog).toContain(DEFAULT_ORGANIZER_ZONE);
    const offsets = catalog.map((z) => zoneOffsetMinutes(z, WINTER_MS));
    for (let i = 1; i < catalog.length; i++) {
      if (offsets[i] === offsets[i - 1]) {
        expect(catalog[i] > catalog[i - 1]).toBe(true);
      } else {
        expect(offsets[i]).toBeGreaterThan(offsets[i - 1]);
      }
    }
  });

  it("среда без supportedValuesOf или с пустым списком — гарантированный Europe/Moscow", () => {
    const spy = vi.spyOn(Intl, "supportedValuesOf");
    try {
      spy.mockImplementation(() => {
        throw new Error("nope");
      });
      expect(zoneCatalog(WINTER_MS)).toEqual([DEFAULT_ORGANIZER_ZONE]);
      spy.mockReturnValue([]);
      expect(zoneCatalog(WINTER_MS)).toEqual([DEFAULT_ORGANIZER_ZONE]);
    } finally {
      spy.mockRestore();
    }
  });
});

describe("isValidIanaZone", () => {
  it("валидное IANA-имя vs мусор", () => {
    expect(isValidIanaZone("Europe/Moscow")).toBe(true);
    expect(isValidIanaZone("Europe/Moscw")).toBe(false);
    expect(isValidIanaZone("")).toBe(false);
  });
});

describe("resolveViewerZone — разрешение Пояса просмотра (спека #3)", () => {
  it("сохранённая валидная → она, битая/пустая → браузерная, обе битые → \"\"", () => {
    expect(resolveViewerZone("Asia/Tokyo", "Europe/Berlin")).toBe("Asia/Tokyo");
    expect(resolveViewerZone("Europe/Moscw", "Europe/Berlin")).toBe("Europe/Berlin");
    expect(resolveViewerZone("", "Europe/Berlin")).toBe("Europe/Berlin");
    expect(resolveViewerZone("Europe/Moscw", "Mars/Olympus")).toBe("");
    expect(resolveViewerZone("", "")).toBe("");
  });
});
