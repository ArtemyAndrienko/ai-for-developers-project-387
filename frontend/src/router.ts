// Мини-роутер: маршрут берётся из hash (#/, #/book, #/create, #/mine).
// Чистая функция — покрыта тестом; ничего не знает про DOM.

export const KNOWN_ROUTES = ["", "book", "create", "mine"] as const;
export type Route = (typeof KNOWN_ROUTES)[number];

export function routeFromHash(hash: string): Route {
  const name = hash.replace(/^#\/?/, "").replace(/\/$/, "").split("?")[0];
  return (KNOWN_ROUTES as readonly string[]).includes(name) ? (name as Route) : "";
}
