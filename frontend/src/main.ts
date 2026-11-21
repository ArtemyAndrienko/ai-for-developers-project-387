import { routeFromHash, type Route } from "./router";
import { renderLanding } from "./views/landing";
import { renderBook } from "./views/book";
import { renderCreate } from "./views/create";
import { renderMine } from "./views/mine";

const app = document.querySelector<HTMLElement>("#app")!;
const tabs = Array.from(document.querySelectorAll<HTMLButtonElement>(".tab"));

const views: Record<Route, (host: HTMLElement) => Promise<void>> = {
  "": renderLanding,
  book: renderBook,
  create: renderCreate,
  mine: renderMine,
};

async function show(route: Route): Promise<void> {
  tabs.forEach((t) => t.classList.toggle("active", t.dataset.view === route));
  app.innerHTML = `<div class="hint">Загрузка…</div>`;
  try {
    await views[route](app);
  } catch (e) {
    app.innerHTML = `<div class="error">Не удалось загрузить данные: ${
      e instanceof Error ? e.message : String(e)
    }. Запущен ли бэкенд (mise run dev-backend)?</div>`;
  }
}

tabs.forEach((t) => t.addEventListener("click", () => {
  location.hash = t.dataset.view === "" ? "#/" : `#/${t.dataset.view}`;
}));

window.addEventListener("hashchange", () => void show(routeFromHash(location.hash)));
void show(routeFromHash(location.hash));
