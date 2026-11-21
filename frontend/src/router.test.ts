import { describe, expect, it } from "vitest";
import { routeFromHash } from "./router";

describe("routeFromHash", () => {
  it("пустой путь и мусор дают лендинг", () => {
    expect(routeFromHash("")).toBe("");
    expect(routeFromHash("#")).toBe("");
    expect(routeFromHash("#/")).toBe("");
    expect(routeFromHash("#/nope")).toBe("");
  });

  it("известные маршруты распознаются", () => {
    expect(routeFromHash("#/book")).toBe("book");
    expect(routeFromHash("#/create")).toBe("create");
    expect(routeFromHash("#/mine")).toBe("mine");
  });

  it("хвостовые запросы и слеши не ломают маршрут", () => {
    expect(routeFromHash("#/book/")).toBe("book");
    expect(routeFromHash("#/mine?x=1")).toBe("mine");
  });
});
