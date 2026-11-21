// Лендинг (референс, скриншот 1): открывается по умолчанию, CTA «Записаться»
// ведёт на страницу записи (#/book).

export async function renderLanding(app: HTMLElement): Promise<void> {
  app.innerHTML = `
    <section class="hero">
      <div class="hero-copy">
        <span class="badge">Быстрая запись на звонок</span>
        <h1>Календарь звонков</h1>
        <p>
          Один экран, понятные интервалы, быстрая бронь. Выберите время и
          запишитесь на звонок без лишних шагов.
        </p>
        <button class="btn primary" id="cta">Записаться →</button>
      </div>
      <div class="card features">
        <h3>Что доступно прямо сейчас</h3>
        <ul>
          <li>Фиксированные 30-минутные интервалы с 09:00 до 18:00 — границы дня
            организатор может задать на конкретную дату</li>
          <li>Проверка конфликта при бронировании: время организатора занято во всех
            его встречах</li>
          <li>Просмотр предстоящих и прошедших записей в отдельном разделе</li>
        </ul>
      </div>
    </section>`;
  app.querySelector<HTMLButtonElement>("#cta")!.addEventListener("click", () => {
    location.hash = "#/book";
  });
}
