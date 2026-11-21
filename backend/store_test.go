package main

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s := NewStore()
	t.Cleanup(func() { _ = s.Close() })
	// Фиксируем «сейчас»: понедельник 16.11.2026, 12:00 UTC. Часы тестов
	// ставятся явно в UTC — окружение раннера не влияет на вердикты (ADR-0004).
	fixed := time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return fixed }
	return s
}

func intPtr(v int) *int { return &v }

func mustMeeting(t *testing.T, s *Store, name string, dur int, owner, date string) *Meeting {
	t.Helper()
	return mustMeetingTZ(t, s, name, dur, owner, date, "")
}

func mustMeetingTZ(t *testing.T, s *Store, name string, dur int, owner, date, timezone string) *Meeting {
	t.Helper()
	m, err := s.CreateMeeting(name, "", dur, nil, owner, "", date, timezone)
	if err != nil {
		t.Fatalf("CreateMeeting(%q): %v", name, err)
	}
	return m
}

func mustCancelMeeting(t *testing.T, s *Store, id string) {
	t.Helper()
	if err := s.DeleteMeeting(id); err != nil {
		t.Fatalf("DeleteMeeting(%s): %v", id, err)
	}
}

func freeStarts(t *testing.T, s *Store, meetingID string) map[string]SlotStatus {
	t.Helper()
	slots, ok := s.DaySlots(meetingID)
	if !ok {
		t.Fatalf("встреча %s не найдена", meetingID)
	}
	got := map[string]SlotStatus{}
	for _, sl := range slots {
		got[sl.Start] = sl.Status
	}
	return got
}

func TestBook_HappyPath(t *testing.T) {
	s := newTestStore(t)
	m := mustMeeting(t, s, "Звонок", 30, "boss@x.io", "2026-11-17")

	b, err := s.Book(m.ID, "09:00", "Иван", "ivan@example.com")
	if err != nil {
		t.Fatalf("первая бронь: %v", err)
	}
	if b.End != "09:30" || b.Date != "2026-11-17" {
		t.Errorf("бронь наследует дату встречи и окончание = старт + длительность: %+v", b)
	}

	// Второй посетитель на тот же интервал — конфликт.
	if _, err := s.Book(m.ID, "09:00", "Оля", "olya@example.com"); !errors.Is(err, ErrSlotTaken) {
		t.Errorf("повторная бронь занятого интервала: %v", err)
	}
	// Следующий интервал — свободен.
	if _, err := s.Book(m.ID, "09:30", "Оля", "olya@example.com"); err != nil {
		t.Errorf("бронь следующего интервала: %v", err)
	}
	av, ok := s.MeetingAvailability(m.ID)
	if !ok || av.Date != "2026-11-17" || av.Free != 16 {
		t.Errorf("после двух броней осталось 16 свободных, получили %+v", av)
	}
}

// Занятость организатора сквозная: запись в одну встречу блокирует пересекающиеся
// интервалы в других его встречах на ту же дату.
func TestBook_OwnerBusyAcrossMeetings(t *testing.T) {
	s := newTestStore(t)
	m30 := mustMeeting(t, s, "Короткий колл", 30, "boss@x.io", "2026-11-17")
	m60 := mustMeeting(t, s, "Часовой разбор", 60, "boss@x.io", "2026-11-17")
	mOther := mustMeeting(t, s, "Чужая встреча", 60, "other@x.io", "2026-11-17")

	if _, err := s.Book(m30.ID, "10:30", "Иван", "ivan@example.com"); err != nil {
		t.Fatal(err)
	}

	statuses := freeStarts(t, s, m60.ID)
	if statuses["10:00"] != SlotTaken {
		t.Errorf("интервал 10:00–11:00 пересекается с 10:30–11:00 организатора и должен быть занят: %s", statuses["10:00"])
	}
	if statuses["09:00"] != SlotFree || statuses["11:00"] != SlotFree {
		t.Errorf("непересекающиеся интервалы должны быть свободны: %v", statuses)
	}
	if _, err := s.Book(m60.ID, "10:00", "Оля", "olya@example.com"); !errors.Is(err, ErrSlotTaken) {
		t.Errorf("бронь пересекающегося интервала другой встречи того же организатора: %v", err)
	}

	// Другой организатор в это же время не блокируется.
	if _, err := s.Book(mOther.ID, "10:00", "Оля", "olya@example.com"); err != nil {
		t.Errorf("другой организатор не должен блокироваться: %v", err)
	}

	// Обратное пересечение: после 09:00–10:00 в 60-мин сетке интервал 09:00–09:30 занят.
	if _, err := s.Book(m60.ID, "09:00", "Петя", "petya@example.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Book(m30.ID, "09:00", "Ваня", "vanya@example.com"); !errors.Is(err, ErrSlotTaken) {
		t.Errorf("пересечение обратно тоже должно блокировать: %v", err)
	}
}

// Границы рабочего дня — по почте организатора на конкретную дату встречи;
// дефолт 09:00–18:00; другие даты и другие организаторы не затрагиваются.
func TestWorkHours_PerOwnerPerDate(t *testing.T) {
	s := newTestStore(t)

	// Дефолт.
	h := s.WorkHours("boss@x.io", "2026-11-17")
	if h.WorkStart != "09:00" || h.WorkEnd != "18:00" {
		t.Fatalf("дефолтные границы: %+v", h)
	}

	// Сохраняем границы только на 17.11 для boss@x.io.
	if err := s.SetWorkHours("boss@x.io", "2026-11-17", "10:00", "12:00"); err != nil {
		t.Fatal(err)
	}
	m := mustMeeting(t, s, "Узкое окно", 30, "boss@x.io", "2026-11-17")
	slots, _ := s.DaySlots(m.ID)
	if len(slots) != 4 { // 10:00, 10:30, 11:00, 11:30
		t.Fatalf("в сохранённых границах 10:00–12:00 должно быть 4 интервала, получили %d", len(slots))
	}
	if slots[0].Start != "10:00" || slots[3].End != "12:00" {
		t.Errorf("сетка не по границам дня: %v … %v", slots[0], slots[3])
	}

	// Та же почта, другая дата — по-прежнему дефолт.
	m2 := mustMeeting(t, s, "Обычный день", 30, "boss@x.io", "2026-11-18")
	if slots2, _ := s.DaySlots(m2.ID); len(slots2) != 18 {
		t.Errorf("на 18.11 границы не должны были измениться, интервалов %d", len(slots2))
	}
	// Другой организатор на 17.11 — дефолт.
	if h := s.WorkHours("other@x.io", "2026-11-17"); h.WorkStart != "09:00" {
		t.Errorf("границы не должны протекать между организаторами: %+v", h)
	}

	// Валидация.
	if err := s.SetWorkHours("boss@x.io", "2026-11-17", "18:00", "09:00"); err == nil {
		t.Error("окончание раньше начала должно отклоняться")
	}
	if err := s.SetWorkHours("not-an-email", "2026-11-17", "10:00", "12:00"); !errors.Is(err, ErrOwnerRequired) {
		t.Errorf("email организатора обязателен: %v", err)
	}
	if err := s.SetWorkHours("boss@x.io", "2026-13-45", "10:00", "12:00"); err == nil {
		t.Error("некорректная дата должна отклоняться")
	}
}

// Превью создаваемой встречи: границы для (owner, date) + занятость организатора.
func TestPreviewSlots(t *testing.T) {
	s := newTestStore(t)
	m30 := mustMeeting(t, s, "Звонок", 30, "boss@x.io", "2026-11-17")
	if _, err := s.Book(m30.ID, "10:30", "Иван", "ivan@example.com"); err != nil {
		t.Fatal(err)
	}

	slots := s.PreviewSlots("boss@x.io", "2026-11-17", 60, 0, "")
	found := false
	for _, sl := range slots {
		if sl.Start == "10:00" {
			found = true
			if sl.Status != SlotTaken {
				t.Errorf("в превью интервал 10:00 должен быть занят, статус %s", sl.Status)
			}
		}
	}
	if !found {
		t.Fatal("в превью нет интервала 10:00")
	}

	// Без owner превью не учитывает чужие брони.
	for _, sl := range s.PreviewSlots("", "2026-11-17", 60, 0, "") {
		if sl.Start == "10:00" && sl.Status != SlotFree {
			t.Error("без email организатора превью не должно показывать занятость")
		}
	}

	// Границы конкретной даты применяются и в превью.
	if err := s.SetWorkHours("boss@x.io", "2026-11-19", "14:00", "18:00"); err != nil {
		t.Fatal(err)
	}
	pv := s.PreviewSlots("boss@x.io", "2026-11-19", 30, 0, "")
	if pv[0].Start != "14:00" {
		t.Errorf("превью должно использовать границы 14:00–18:00, первый интервал %s", pv[0].Start)
	}
}

func TestCreateMeeting_Validation(t *testing.T) {
	s := newTestStore(t)
	cases := []struct {
		name       string
		mname, own string
		dur        int
		date       string
		wantErr    error
	}{
		{"без названия", "", "boss@x.io", 30, "2026-11-17", nil},
		{"нулевая длительность", "x", "boss@x.io", 0, "2026-11-17", nil},
		{"без email организатора", "x", "", 30, "2026-11-17", ErrOwnerRequired},
		{"некорректный email", "x", "nope", 30, "2026-11-17", ErrOwnerRequired},
		{"дата в прошлом", "x", "boss@x.io", 30, "2026-11-10", ErrDateRequired},
		{"дата вне окна", "x", "boss@x.io", 30, "2026-12-31", ErrDateRequired},
		{"кривая дата", "x", "boss@x.io", 30, "17.11.2026", ErrDateRequired},
	}
	for _, c := range cases {
		_, err := s.CreateMeeting(c.mname, "", c.dur, nil, c.own, "", c.date, "")
		if err == nil {
			t.Errorf("%s: ошибки нет", c.name)
		}
		if c.wantErr != nil && !errors.Is(err, c.wantErr) {
			t.Errorf("%s: got %v, want %v", c.name, err, c.wantErr)
		}
	}
}

func TestMeetingsFilterByOwner(t *testing.T) {
	s := newTestStore(t)
	mustMeeting(t, s, "Поздняя", 30, "boss@x.io", "2026-11-20")
	mustMeeting(t, s, "Ранняя", 30, "BOSS@X.IO", "2026-11-17") // email нечувствителен к регистру
	mustMeeting(t, s, "Чужая", 30, "other@x.io", "2026-11-18")
	got := s.Meetings("boss@x.io")
	if len(got) != 2 || got[0].Name != "Ранняя" || got[1].Name != "Поздняя" {
		t.Errorf("фильтр/сортировка по организатору: %+v", got)
	}
	if all := s.Meetings(""); len(all) != 3 {
		t.Errorf("без фильтра — все встречи: %d", len(all))
	}
}

// Организатор: имя необязательное, хранится вместе со встречей и
// отображается посетителю в «Мои записи».
func TestMeetingOrganizerName(t *testing.T) {
	s := newTestStore(t)
	m, err := s.CreateMeeting("Разбор", "", 30, nil, "boss@x.io", "Иван Босс", "2026-11-17", "")
	if err != nil {
		t.Fatal(err)
	}
	if m.OwnerName != "Иван Босс" || m.OwnerEmail != "boss@x.io" {
		t.Errorf("имя организатора не сохранилось: %+v", m)
	}
	// Без имени — пустая строка допустима.
	m2, err := s.CreateMeeting("Аноним", "", 30, nil, "boss@x.io", "", "2026-11-18", "")
	if err != nil || m2.OwnerName != "" {
		t.Errorf("имя организатора опционально: %+v %v", m2, err)
	}
}

func TestPreviewAndDeleteMeeting(t *testing.T) {
	s := newTestStore(t)
	avail := s.PreviewAvailability(30, 0, "", "")
	if len(avail) != windowDays || avail[0].Date != "2026-11-16" || avail[13].Date != "2026-11-29" {
		t.Fatalf("окно превью: %+v", avail)
	}
	if avail[0].Free != 11 { // сегодня: сейчас 12:00 → интервалы с 12:30 по 17:30
		t.Errorf("на сегодня 11 свободных, получили %d", avail[0].Free)
	}

	m := mustMeeting(t, s, "Звонок", 30, "boss@x.io", "2026-11-17")
	if _, err := s.Book(m.ID, "09:00", "Иван", "ivan@example.com"); err != nil {
		t.Fatal(err)
	}
	mustCancelMeeting(t, s, m.ID)
	// Мягкая Отмена Встречи: Записи не стираются, а переходят в «Отменена».
	if bs := s.BookingsByUser("ivan@example.com"); len(bs) != 1 || bs[0].State != BookingCancelled {
		t.Errorf("после Отмены Встречи её Записи должны стать «Отменена»: %+v", bs)
	}
}

func TestBook_Rejections(t *testing.T) {
	s := newTestStore(t)
	today := mustMeeting(t, s, "Сегодня", 30, "boss@x.io", "2026-11-16")
	future := mustMeeting(t, s, "Будущая", 30, "boss@x.io", "2026-11-17")

	if _, err := s.Book(today.ID, "10:00", "Иван", "ivan@x.io"); !errors.Is(err, ErrSlotUnavailable) {
		t.Errorf("прошедший интервал сегодняшнего дня: %v", err)
	}
	if _, err := s.Book(future.ID, "09:15", "Иван", "ivan@x.io"); !errors.Is(err, ErrSlotUnavailable) {
		t.Errorf("интервал вне сетки: %v", err)
	}
	if _, err := s.Book(future.ID, "09:00", "", "ivan@x.io"); !errors.Is(err, ErrNameRequired) {
		t.Errorf("пустое имя: %v", err)
	}
	if _, err := s.Book(future.ID, "09:00", "Иван", "nope"); !errors.Is(err, ErrEmailRequired) {
		t.Errorf("некорректный email посетителя: %v", err)
	}
	if _, err := s.Book("m-nope", "09:00", "n", "e@x.io"); !errors.Is(err, ErrMeetingNotFound) {
		t.Errorf("несуществующая встреча: %v", err)
	}
	// «Сейчас» переезжает за пределы окна доступности — дата существующей
	// встречи устаревает, бронь должна отклоняться.
	stale := mustMeeting(t, s, "Просроченная", 30, "boss@x.io", "2026-11-17")
	s.now = func() time.Time { return time.Date(2026, 12, 20, 12, 0, 0, 0, time.UTC) }
	if _, err := s.Book(stale.ID, "09:00", "Иван", "ivan@x.io"); !errors.Is(err, ErrOutsideWindow) {
		t.Errorf("бронь встречи вне окна: %v", err)
	}
}

// Отмена переживает перезакрытие базы: состояние и освобождённый Интервал
// берутся из файла, миграции не повторяются.
func TestCancelBooking_PersistsAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "calendar.db")
	fixed := func() time.Time { return time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC) }

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s.now = fixed
	m := mustMeeting(t, s, "Переживает рестарт", 30, "boss@x.io", "2026-11-17")
	if _, err := s.Book(m.ID, "09:00", "Петя", "petya@example.com"); err != nil {
		t.Fatal(err)
	}
	b, err := s.Book(m.ID, "10:00", "Иван", "ivan@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CancelBooking(b.ID, "ivan@example.com"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	s2, err := Open(path) // fresh соединение поверх WAL
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	s2.now = fixed

	bs := s2.BookingsByUser("ivan@example.com")
	if len(bs) != 1 || bs[0].ID != b.ID || bs[0].State != BookingCancelled {
		t.Errorf("отменённая запись не пережила рестарт: %+v", bs)
	}
	// После рестарта чужая активная запись по-прежнему занимает свой Интервал…
	if _, err := s2.Book(m.ID, "09:00", "Оля", "olya@example.com"); !errors.Is(err, ErrSlotTaken) {
		t.Errorf("активная запись должна оставаться занятой: %v", err)
	}
	// …а освобождённый Интервал снова свободен и для прежнего посетителя.
	if _, err := s2.Book(m.ID, "10:00", "Иван", "ivan@example.com"); err != nil {
		t.Errorf("освобождённый Интервал после рестарта должен быть доступен: %v", err)
	}
	// …а активная запись Пети осталась active.
	if ps := s2.BookingsByUser("petya@example.com"); len(ps) != 1 || ps[0].State != BookingActive {
		t.Errorf("активная запись после рестарта должна остаться active: %+v", ps)
	}
	var migrations int
	if err := s2.db.QueryRow(`SELECT count(*) FROM schema_version`).Scan(&migrations); err != nil {
		t.Fatal(err)
	}
	if migrations != 5 {
		t.Errorf("миграции должны примениться по одному разу, посчитано %d", migrations)
	}
}

// Отмена Встречи Организатором (GLOSSARY: «Отмена Встречи») — мягкая:
// Встреча переживает отмену со статусом cancelled, её активные Записи
// переходят в «Отменена» в одной транзакции и остаются в истории Посетителя.
func TestCancelMeeting_MarksBookingsCancelled(t *testing.T) {
	s := newTestStore(t)
	m := mustMeeting(t, s, "Звонок", 30, "boss@x.io", "2026-11-17")
	b1, err := s.Book(m.ID, "09:00", "Иван", "ivan@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Book(m.ID, "10:00", "Оля", "olya@example.com"); err != nil {
		t.Fatal(err)
	}

	mustCancelMeeting(t, s, m.ID)

	mm, ok := s.Meeting(m.ID)
	if !ok || mm.State != MeetingCancelled {
		t.Errorf("Встреча должна пережить Отмену со статусом cancelled: %+v ok=%v", mm, ok)
	}
	bs := s.BookingsByUser("ivan@example.com")
	if len(bs) != 1 || bs[0].ID != b1.ID || bs[0].State != BookingCancelled {
		t.Errorf("Запись должна остаться в истории отменённой: %+v", bs)
	}
	if os := s.BookingsByUser("olya@example.com"); len(os) != 1 || os[0].State != BookingCancelled {
		t.Errorf("все активные Записи Встречи должны стать «Отменена»: %+v", os)
	}
}

// Отменённая Встреча исчезает из публичного списка, из «ваших встреч»
// организатора и из продажи: её сетка и доступность больше не отдаются
// (handler отвечает 404 — «нет в продаже», данные нужны только истории).
func TestCancelMeeting_HiddenFromLists(t *testing.T) {
	s := newTestStore(t)
	mustMeeting(t, s, "Чужая", 30, "other@x.io", "2026-11-18")
	m := mustMeeting(t, s, "Звонок", 30, "boss@x.io", "2026-11-17")
	mustCancelMeeting(t, s, m.ID)

	if all := s.Meetings(""); len(all) != 1 || all[0].OwnerEmail != "other@x.io" {
		t.Errorf("отменённая Встреча не должна быть в публичном списке: %+v", all)
	}
	if own := s.Meetings("boss@x.io"); len(own) != 0 {
		t.Errorf("отменённая Встреча не должна быть в списке организатора: %+v", own)
	}
	if slots, ok := s.DaySlots(m.ID); ok {
		t.Errorf("сетка отменённой Встречи недоступна (404), получили %v", slots)
	}
	if av, ok := s.MeetingAvailability(m.ID); ok {
		t.Errorf("доступность отменённой Встречи недоступна (404), получили %+v", av)
	}
}

// Повторная Отмена Встречи идемпотентна: ничего не меняется, ошибок нет
// (handler отвечает тем же 204).
func TestCancelMeeting_Idempotent(t *testing.T) {
	s := newTestStore(t)
	m := mustMeeting(t, s, "Звонок", 30, "boss@x.io", "2026-11-17")
	if _, err := s.Book(m.ID, "09:00", "Иван", "ivan@example.com"); err != nil {
		t.Fatal(err)
	}

	mustCancelMeeting(t, s, m.ID)
	// Второй раз — успешный no-op без ошибки (handler отвечает тот же 204).
	mustCancelMeeting(t, s, m.ID)
	// Несуществующая Встреча — различимая ошибка (handler — 404).
	if err := s.DeleteMeeting("m-nope"); !errors.Is(err, ErrMeetingNotFound) {
		t.Errorf("отмена несуществующей Встречи: %v, хотим ErrMeetingNotFound", err)
	}

	if mm, ok := s.Meeting(m.ID); !ok || mm.State != MeetingCancelled {
		t.Errorf("повторная Отмена не должна менять Встречу: %+v ok=%v", mm, ok)
	}
	bs := s.BookingsByUser("ivan@example.com")
	if len(bs) != 1 || bs[0].State != BookingCancelled {
		t.Errorf("повторная Отмена не должна менять Записи: %+v", bs)
	}
}

// «Хвостовой интервал» упразднён и на Store-шовне: день 09:00–17:50 с
// длительностью 45 даёт 11 целых стартов (последний 16:30–17:15), остаток
// 17:15–17:50 в сетке отсутствует, а бронь не-старта 17:15 отклоняется как
// интервал вне сетки.
func TestBook_TailNeverAppears(t *testing.T) {
	s := newTestStore(t)
	if err := s.SetWorkHours("boss@x.io", "2026-11-17", "09:00", "17:50"); err != nil {
		t.Fatal(err)
	}
	m := mustMeeting(t, s, "Долгий созвон", 45, "boss@x.io", "2026-11-17")
	slots, _ := s.DaySlots(m.ID)
	if len(slots) != 11 {
		t.Fatalf("ожидалось 11 целых интервалов, получено %d", len(slots))
	}
	last := slots[len(slots)-1]
	if last.Start != "16:30" || last.End != "17:15" || last.Status != SlotFree {
		t.Errorf("последний интервал должен быть целым и свободным: %v", last)
	}
	if _, err := s.Book(m.ID, "17:15", "Иван", "ivan@example.com"); !errors.Is(err, ErrSlotUnavailable) {
		t.Errorf("бронь не влезающего старта: %v, хотим ErrSlotUnavailable", err)
	}
}

// Плотная сетка 60/30 (ADR-0003): одна Запись блокирует ВСЕ пересекающиеся
// старты — бронь 10:00–11:00 занимает старты 09:30, 10:00 и 10:30,
// а касающиеся границей 09:00 и 11:00 остаются свободны.
func TestDenseGrid_BookingBlocksAllOverlappingStarts(t *testing.T) {
	s := newTestStore(t)
	m, err := s.CreateMeeting("Часовой созвон", "", 60, intPtr(30), "boss@x.io", "", "2026-11-17", "")
	if err != nil {
		t.Fatal(err)
	}
	if m.StartStepMin == nil || *m.StartStepMin != 30 {
		t.Fatalf("шаг не сохранился: %+v", m)
	}
	if status := freeStarts(t, s, m.ID); len(status) != 17 {
		t.Errorf("сетка 60/30 на 09:00–18:00: ожидалось 17 стартов, получено %d", len(status))
	}

	if _, err := s.Book(m.ID, "10:00", "Иван", "ivan@example.com"); err != nil {
		t.Fatal(err)
	}
	statuses := freeStarts(t, s, m.ID)
	for _, start := range []string{"09:30", "10:00", "10:30"} {
		if statuses[start] != SlotTaken {
			t.Errorf("старт %s пересекается с бронью 10:00–11:00 и должен быть занят: %s", start, statuses[start])
		}
	}
	for _, start := range []string{"09:00", "11:00"} {
		if statuses[start] != SlotFree {
			t.Errorf("старт %s лишь касается брони и должен быть свободен: %s", start, statuses[start])
		}
	}
	// Повторная запись на пересекающийся старт отклоняется как занятая.
	if _, err := s.Book(m.ID, "10:30", "Оля", "olya@example.com"); !errors.Is(err, ErrSlotTaken) {
		t.Errorf("бронь пересекающегося старта плотной сетки: %v, хотим ErrSlotTaken", err)
	}
}

// Шаг по умолчанию: не задан (= NULL) — сетка как раньше, старт = длительность.
func TestCreateMeeting_StartStepDefaultsToDuration(t *testing.T) {
	s := newTestStore(t)
	m, err := s.CreateMeeting("Обычный", "", 60, nil, "boss@x.io", "", "2026-11-17", "")
	if err != nil {
		t.Fatal(err)
	}
	if m.StartStepMin != nil {
		t.Errorf("без шага ждём nil, получили %v", *m.StartStepMin)
	}
	if status := freeStarts(t, s, m.ID); len(status) != 9 { // 09:00…17:00 с шагом 60
		t.Errorf("шаг = длительность: ожидалось 9 интервалов, получено %d", len(status))
	}
}

// Серверная валидация (store-шовен): шаг — целый 1..длительности включительно.
func TestCreateMeeting_StartStepValidation(t *testing.T) {
	s := newTestStore(t)
	for _, step := range []int{0, -5, 61, 120} {
		if _, err := s.CreateMeeting("x", "", 60, intPtr(step), "boss@x.io", "", "2026-11-17", ""); !errors.Is(err, ErrStartStepRange) {
			t.Errorf("шаг %d при длительности 60: %v, хотим ErrStartStepRange", step, err)
		}
	}
	for _, step := range []int{1, 15, 60} {
		if _, err := s.CreateMeeting("x", "", 60, intPtr(step), "boss@x.io", "", "2026-11-17", ""); err != nil {
			t.Errorf("шаг %d при длительности 60 должен приниматься: %v", step, err)
		}
	}
}

// Шаг переживает перезакрытие базы: значение и NULL (дефолт) читаются из файла.
func TestStartStep_PersistsAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "calendar.db")
	fixed := func() time.Time { return time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC) }

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s.now = fixed
	stepMeeting, err := s.CreateMeeting("Плотный", "", 60, intPtr(30), "boss@x.io", "", "2026-11-17", "")
	if err != nil {
		t.Fatal(err)
	}
	plainMeeting := mustMeeting(t, s, "Простой", 60, "boss@x.io", "2026-11-18")
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	s2.now = fixed

	m1, ok := s2.Meeting(stepMeeting.ID)
	if !ok || m1.StartStepMin == nil || *m1.StartStepMin != 30 {
		t.Errorf("шаг 30 должен пережить рестарт: %+v ok=%v", m1, ok)
	}
	if status := freeStarts(t, s2, m1.ID); len(status) != 17 {
		t.Errorf("после рестарта сетка плотная: ожидалось 17 стартов, получено %d", len(status))
	}
	m2, ok := s2.Meeting(plainMeeting.ID)
	if !ok || m2.StartStepMin != nil {
		t.Errorf("встреча без шага: ждём nil, получили %+v", m2)
	}
}

// Превью создаваемой встречи понимает шаг: 60/30 — старты каждые 30 минут.
func TestPreviewSlots_StartStep(t *testing.T) {
	s := newTestStore(t)
	plain := s.PreviewSlots("boss@x.io", "2026-11-17", 60, 0, "")
	if len(plain) != 9 {
		t.Errorf("без шага (0 = длительность): ожидалось 9 интервалов, получено %d", len(plain))
	}
	dense := s.PreviewSlots("boss@x.io", "2026-11-17", 60, 30, "")
	if len(dense) != 17 {
		t.Fatalf("шаг 30: ожидалось 17 интервалов, получено %d", len(dense))
	}
	if dense[1].Start != "09:30" || dense[1].End != "10:30" {
		t.Errorf("второй старт плотной сетки %v", dense[1])
	}
	if last := dense[len(dense)-1]; last.Start != "17:00" || last.End != "18:00" {
		t.Errorf("последний старт плотной сетки %v", last)
	}

	// Занятость организатора просвечивает и в плотное превью.
	m, err := s.CreateMeeting("Часовой созвон", "", 60, intPtr(30), "boss@x.io", "", "2026-11-17", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Book(m.ID, "10:00", "Иван", "ivan@example.com"); err != nil {
		t.Fatal(err)
	}
	taken := map[string]SlotStatus{}
	for _, sl := range s.PreviewSlots("boss@x.io", "2026-11-17", 60, 30, "") {
		taken[sl.Start] = sl.Status
	}
	if taken["10:00"] != SlotTaken || taken["10:30"] != SlotTaken {
		t.Errorf("превью с шагом должно показывать занятость пересекающихся стартов: %v", taken)
	}

	// Окно доступности тоже считается по шагу.
	avail := s.PreviewAvailability(60, 30, "", "")
	if len(avail) != windowDays || avail[1].Free != 17 {
		t.Errorf("плотное окно: %+v", avail)
	}
}

// Превью до первого сохранения: приоритет зоны — переданная > сохранённый
// профиль > UTC. Один момент «сейчас» (23:30 UTC) — у Колкаты уже утро 17.11,
// в UTC всё ещё 16.11: без параметра превьи жили бы чужими часами.
func TestPreviewSlots_TimezoneFallback(t *testing.T) {
	s := newTestStore(t)
	s.now = func() time.Time { return time.Date(2026, 11, 16, 23, 30, 0, 0, time.UTC) }
	mustMeetingTZ(t, s, "Колката", 30, "kol@x.io", "2026-11-17", "Asia/Kolkata")

	// Окно: переданная зона важнее сохранённого профиля.
	fromParam := s.PreviewAvailability(30, 0, "kol@x.io", "UTC")
	fromProfile := s.PreviewAvailability(30, 0, "kol@x.io", "")
	fromUTC := s.PreviewAvailability(30, 0, "fresh@x.io", "")
	if fromParam[0].Date != "2026-11-16" {
		t.Errorf("переданная зона должна быть важнее профиля: окно открыт %s", fromParam[0].Date)
	}
	if fromProfile[0].Date != "2026-11-17" {
		t.Errorf("без параметра окно считается по сохранённому профилю: %s", fromProfile[0].Date)
	}
	if fromUTC[0].Date != "2026-11-16" {
		t.Errorf("ни параметра, ни профиля — детерминированный UTC: %s", fromUTC[0].Date)
	}

	// Некорректный фолбэк ведёт себя как непереданный — профиль важнее.
	bad := s.PreviewAvailability(30, 0, "kol@x.io", "Mars/Olympus")
	if bad[0].Date != fromProfile[0].Date {
		t.Errorf("некорректная зона-фолбэк не ломает превью: %s", bad[0].Date)
	}

	// Сетка того же дня — теми же часами: утро 17.11 по Колкате уже наступило
	// (ранние старты 04:00–05:00 прошла), а в UTC-фолбэке день будущий.
	if err := s.SetWorkHours("kol@x.io", "2026-11-17", "04:00", "09:00"); err != nil {
		t.Fatal(err)
	}
	startStatus := func(zone string) map[string]SlotStatus {
		got := map[string]SlotStatus{}
		for _, sl := range s.PreviewSlots("kol@x.io", "2026-11-17", 30, 0, zone) {
			got[sl.Start] = sl.Status
		}
		return got
	}
	profileGrid := startStatus("")
	if profileGrid["04:00"] != SlotPast || profileGrid["08:30"] != SlotFree {
		t.Errorf("по часам профиля 04:00 уже прошло, 08:30 свободно: %v", profileGrid)
	}
	utcGrid := startStatus("UTC")
	if utcGrid["04:00"] != SlotFree {
		t.Errorf("в UTC-фолбэке 17.11 — будущий день, 04:00 свободен: %v", utcGrid)
	}
}

// Сценарий 2: после окончания Встречи её Запись попадает в «Прошедшие».
func TestIsPast_Grouping(t *testing.T) {
	s := newTestStore(t)
	m := mustMeeting(t, s, "Звонок", 30, "boss@x.io", "2026-11-17")
	future, err := s.Book(m.ID, "09:00", "Иван", "ivan@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if s.IsPast(future) {
		t.Errorf("будущая встреча не должна быть past")
	}

	// Перематываем «сейчас» за окончание брони 09:00–09:30.
	s.now = func() time.Time { return time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC) }
	if !s.IsPast(future) {
		t.Errorf("после окончания Встреча должна попасть в «Прошедшие»")
	}

	m2 := mustMeeting(t, s, "Второй звонок", 30, "boss@x.io", "2026-11-18")
	if _, err := s.Book(m2.ID, "09:00", "Иван", "ivan@example.com"); err != nil {
		t.Fatal(err)
	}
	bs := s.BookingsByUser("ivan@example.com")
	if len(bs) != 2 || bs[0].Date != "2026-11-17" || bs[1].Date != "2026-11-18" {
		t.Errorf("сортировка записей по времени: %+v", bs)
	}
}

// Отмена Записи Посетителем: Интервал мгновенно освобождается, в том числе
// в других Встречах того же Организатора (конфликт по пересечению).
func TestCancelBooking_ReleasesSlot(t *testing.T) {
	s := newTestStore(t)
	m30 := mustMeeting(t, s, "Короткий колл", 30, "boss@x.io", "2026-11-17")
	m60 := mustMeeting(t, s, "Часовой разбор", 60, "boss@x.io", "2026-11-17")

	b, err := s.Book(m30.ID, "10:30", "Иван", "ivan@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Book(m60.ID, "10:00", "Оля", "olya@example.com"); !errors.Is(err, ErrSlotTaken) {
		t.Fatalf("до отмены 10:00–11:00 занят пересечением с 10:30–11:00: %v", err)
	}

	if err := s.CancelBooking(b.ID, "ivan@example.com"); err != nil {
		t.Fatalf("отмена собственной Записи: %v", err)
	}
	if _, err := s.Book(m60.ID, "10:00", "Оля", "olya@example.com"); err != nil {
		t.Errorf("после отмены Интервал свободен во всех Встречах организатора: %v", err)
	}
}

// Story 15: освобождённый отменой Интервал занимает другой Посетитель, а
// следующий на тот же Интервал получает 409-семантику (ErrSlotTaken).
func TestCancelBooking_RaceAfterRelease(t *testing.T) {
	s := newTestStore(t)
	m := mustMeeting(t, s, "Звонок", 30, "boss@x.io", "2026-11-17")
	b, err := s.Book(m.ID, "09:00", "Иван", "ivan@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CancelBooking(b.ID, "ivan@example.com"); err != nil {
		t.Fatalf("отмена собственной Записи: %v", err)
	}
	if _, err := s.Book(m.ID, "09:00", "Оля", "olya@example.com"); err != nil {
		t.Fatalf("освобождённый Интервал должен сразу быть доступен другому: %v", err)
	}
	if _, err := s.Book(m.ID, "09:00", "Петя", "petya@example.com"); !errors.Is(err, ErrSlotTaken) {
		t.Errorf("третий посетитель на занятый Интервал: %v, хотим ErrSlotTaken", err)
	}
}

// Отменить Запись может только совпадающий email Посетителя (самозаявленная
// идентичность до авторизации); колонки email — NOCASE.
func TestCancelBooking_WrongEmailRejected(t *testing.T) {
	s := newTestStore(t)
	m := mustMeeting(t, s, "Звонок", 30, "boss@x.io", "2026-11-17")
	b, err := s.Book(m.ID, "09:00", "Иван", "ivan@example.com")
	if err != nil {
		t.Fatal(err)
	}

	if err := s.CancelBooking(b.ID, "someone@else.io"); !errors.Is(err, ErrBookingEmailMismatch) {
		t.Errorf("чужой email должен отклоняться: %v", err)
	}
	if err := s.CancelBooking("b-999", "ivan@example.com"); !errors.Is(err, ErrBookingNotFound) {
		t.Errorf("несуществующая запись: %v", err)
	}
	// Регистр email не значим.
	if err := s.CancelBooking(b.ID, "IVAN@EXAMPLE.COM"); err != nil {
		t.Errorf("совпадение email без учёта регистра: %v", err)
	}
	// Отклонённая отмена не освободила Интервал — уже проверено выше: Интервал
	// занят до успешной отмены, а после неё свободен.
	if _, err := s.Book(m.ID, "09:00", "Оля", "olya@example.com"); err != nil {
		t.Errorf("после успешной отмены Интервал свободен: %v", err)
	}
}

// Отмена доступна только для предстоящих Записей; завершившаяся — отказ.
func TestCancelBooking_PastRejected(t *testing.T) {
	s := newTestStore(t)
	m := mustMeeting(t, s, "Звонок", 30, "boss@x.io", "2026-11-17")
	b, err := s.Book(m.ID, "09:00", "Иван", "ivan@example.com")
	if err != nil {
		t.Fatal(err)
	}

	// Перематываем «сейчас» за окончание брони 09:00–09:30.
	s.now = func() time.Time { return time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC) }
	if err := s.CancelBooking(b.ID, "ivan@example.com"); !errors.Is(err, ErrBookingPast) {
		t.Errorf("завершившаяся Запись должна отклонять отмену: %v", err)
	}
	// Запись осталась активной.
	bs := s.BookingsByUser("ivan@example.com")
	if len(bs) != 1 || bs[0].State != BookingActive {
		t.Errorf("отклонённая отмена не меняет состояние: %+v", bs)
	}
}

// Повторная отмена той же Записи — успешный no-op (идемпотентность).
func TestCancelBooking_Idempotent(t *testing.T) {
	s := newTestStore(t)
	m := mustMeeting(t, s, "Звонок", 30, "boss@x.io", "2026-11-17")
	b, err := s.Book(m.ID, "09:00", "Иван", "ivan@example.com")
	if err != nil {
		t.Fatal(err)
	}

	if err := s.CancelBooking(b.ID, "ivan@example.com"); err != nil {
		t.Fatal(err)
	}
	// Вторая и третья отмена ничего не меняют и не ошибаются; в том числе
	// после перематывания «сейчас» за окончание — cancelled остаётся no-op.
	s.now = func() time.Time { return time.Date(2026, 11, 17, 12, 0, 0, 0, time.UTC) }
	for i := 0; i < 2; i++ {
		if err := s.CancelBooking(b.ID, "ivan@example.com"); err != nil {
			t.Errorf("повторная отмена #%d: %v", i+1, err)
		}
	}
	bs := s.BookingsByUser("ivan@example.com")
	if len(bs) != 1 || bs[0].State != BookingCancelled {
		t.Errorf("состояние после повторных отмен: %+v", bs)
	}
	// Чужой email для уже отменённой Записи по-прежнему отклоняется.
	if err := s.CancelBooking(b.ID, "someone@else.io"); !errors.Is(err, ErrBookingEmailMismatch) {
		t.Errorf("чужой email на отменённой записи: %v", err)
	}
}

// Отменённая Запись не занимает время Организатора сквозняком: в его других
// Встречах на ту же дату пересекающиеся Интервалы снова свободны, а активные
// брони остаются занятыми.
func TestCancelBooking_IgnoredInOwnerBusy(t *testing.T) {
	s := newTestStore(t)
	m30 := mustMeeting(t, s, "Короткий колл", 30, "boss@x.io", "2026-11-17")
	m60 := mustMeeting(t, s, "Часовой разбор", 60, "boss@x.io", "2026-11-17")

	cancelled, err := s.Book(m30.ID, "10:30", "Иван", "ivan@example.com")
	if err != nil {
		t.Fatal(err)
	}
	kept, err := s.Book(m30.ID, "14:00", "Оля", "olya@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CancelBooking(cancelled.ID, "ivan@example.com"); err != nil {
		t.Fatal(err)
	}

	statuses := freeStarts(t, s, m60.ID)
	if statuses["10:00"] != SlotFree {
		t.Errorf("после отмены интервал 10:00 в другой встрече организатора свободен: %s", statuses["10:00"])
	}
	// Активная запись того же посетителя в сетке 30-минутной встречи —
	// на 14:00 не влияет на 60-мин сетку; проверяем, что она сама по-прежнему занята.
	statuses30 := freeStarts(t, s, m30.ID)
	if statuses30["10:30"] != SlotFree {
		t.Errorf("отменённый интервал снова свободен в исходной встрече: %s", statuses30["10:30"])
	}
	if statuses30["14:00"] != SlotTaken {
		t.Errorf("активная запись остаётся занятой: %s", statuses30["14:00"])
	}

	// Отменённая Запись видна посетителю в состоянии cancelled.
	bs := s.BookingsByUser("ivan@example.com")
	if len(bs) != 1 || bs[0].State != BookingCancelled {
		t.Errorf("отменённая запись в списке посетителя: %+v", bs)
	}
	_ = kept
}

// Профиль Организатора: upsert при сохранении Встречи, чтение в ответе,
// пустая строка зоны не трогает профиль, без профиля — UTC (дефолт
// детерминированный, не локаль процесса). Некорректная IANA-зона отклоняется.
func TestOrganizerTimezone_ProfileUpsertAndRead(t *testing.T) {
	s := newTestStore(t)

	// Профиль создаётся вместе с Встречей и возвращается в её представлении.
	m := mustMeetingTZ(t, s, "Колл", 30, "boss@x.io", "2026-11-17", "Asia/Kolkata")
	if m.Timezone != "Asia/Kolkata" {
		t.Errorf("зона из профиля должна вернуться с Встречей: %q", m.Timezone)
	}

	// Пустой timezone — профиль не трогаем, Встреча живёт в прежней зоне.
	m2 := mustMeeting(t, s, "Второй колл", 30, "boss@x.io", "2026-11-18")
	if m2.Timezone != "Asia/Kolkata" {
		t.Errorf("пустая зона не должна стирать профиль: %q", m2.Timezone)
	}

	// Повторный upsert — зона обновляется у всех Встреч организатора
	// (она читается из профиля на чтении, а не хранится на Встрече).
	m3 := mustMeetingTZ(t, s, "Третий колл", 30, "Boss@x.io", "2026-11-19", "Europe/Berlin")
	if m3.Timezone != "Europe/Berlin" {
		t.Errorf("upsert должен обновить зону: %q", m3.Timezone)
	}
	if reread, ok := s.Meeting(m.ID); !ok || reread.Timezone != "Europe/Berlin" {
		t.Errorf("перечитанная Встреча должна нести актуальную зону профиля: %+v", reread)
	}

	// Без профиля — UTC.
	plain := mustMeeting(t, s, "Без профиля", 30, "other@x.io", "2026-11-17")
	if plain.Timezone != "UTC" {
		t.Errorf("Встреча без профиля организатора должна быть в UTC: %q", plain.Timezone)
	}

	// Некорректная IANA-зона отклоняется и не портит профиль.
	if _, err := s.CreateMeeting("Кривая зона", "", 30, nil, "boss@x.io", "", "2026-11-17", "Mars/Olympus"); err == nil {
		t.Error("некорректная IANA-зона должна отклоняться")
	}
	if again, err := s.CreateMeeting("После отказа", "", 30, nil, "boss@x.io", "", "2026-11-17", ""); err != nil || again.Timezone != "Europe/Berlin" {
		t.Errorf("отклонённое создание не меняет профиль: %+v %v", again, err)
	}
}

// Ядро ADR-0004: один и тот же момент «сейчас» — разные календарные дни
// у организаторов в разных зонах. 23:30 UTC — в Колкате (UTC+5:30) уже
// 05:00 следующего дня, в UTC — всё ещё текущий.
func TestOrganizerTimezone_SameInstantDifferentDay(t *testing.T) {
	s := newTestStore(t)
	s.now = func() time.Time { return time.Date(2026, 11, 16, 23, 30, 0, 0, time.UTC) }

	// Организатор из Колкаты: «сегодня» для него 17.11 — вчерашний 16.11 уже
	// за окном, а сетка на 17.11 утренняя (05:30 по его часам).
	kol := mustMeetingTZ(t, s, "Колката", 30, "kol@x.io", "2026-11-17", "Asia/Kolkata")
	if _, err := s.CreateMeeting("Вчера по UTC", "", 30, nil, "kol@x.io", "", "2026-11-16", ""); !errors.Is(err, ErrDateRequired) {
		t.Errorf("16.11 для Колкаты уже вчера: %v", err)
	}
	statuses := freeStarts(t, s, kol.ID)
	for start, st := range statuses {
		if st != SlotFree {
			t.Errorf("на «утро» Колкаты (%s) все интервалы свободны, получили %s=%s", kol.Date, start, st)
		}
	}

	// Без профиля — UTC: 16.11 ещё «сегодня», и к 23:30 день целиком прошёл.
	utc := mustMeeting(t, s, "ЮТиЦ", 30, "plain@x.io", "2026-11-16")
	if _, err := s.CreateMeeting("Сегодня в UTC", "", 30, nil, "plain@x.io", "", "2026-11-16", ""); err != nil {
		t.Errorf("16.11 в UTC ещё сегодня: %v", err)
	}
	for start, st := range freeStarts(t, s, utc.ID) {
		if st != SlotPast {
			t.Errorf("на 23:30 UTC день 16.11 целиком прошёл (%s=%s)", start, st)
		}
	}

	// Окно доступности начинается с дня организатора.
	availKol := s.PreviewAvailability(30, 0, "kol@x.io", "")
	availPlain := s.PreviewAvailability(30, 0, "plain@x.io", "")
	if availKol[0].Date != "2026-11-17" || availKol[13].Date != "2026-11-30" {
		t.Errorf("окно Колкаты должно быть 17.11–30.11: %+v … %+v", availKol[0], availKol[13])
	}
	if availPlain[0].Date != "2026-11-16" || availPlain[13].Date != "2026-11-29" {
		t.Errorf("окно UTC-организатора должно быть 16.11–29.11: %+v … %+v", availPlain[0], availPlain[13])
	}

	// Бронь у «вчерашнего» для организатора дня отклоняется даже прямо:
	// у Колкаты 16.11 уже за периметром окна.
	if _, err := s.Book(kol.ID, "09:00", "Иван", "ivan@x.io"); err != nil {
		t.Errorf("бронь сегодняшнего для Колкаты дня: %v", err)
	}
	if _, err := s.Book(utc.ID, "09:00", "Иван", "ivan@x.io"); !errors.Is(err, ErrSlotUnavailable) {
		t.Errorf("бронь прошедшего дня UTC-организатора: %v", err)
	}
}

// Вердикт «уже прошло» — по wall-clock организатора (ADR-0004): на границе
// полуночи один и тот же момент даёт разные ответы в зоне и в UTC.
func TestIsPast_OrganizerZoneAcrossMidnight(t *testing.T) {
	s := newTestStore(t)

	// Москва (UTC+3). «Сейчас» 04:00 UTC — там 07:00 того же дня: бронь 09:00
	// в будущем, затем перематываем на 07:00 UTC — в Москве 10:00, финал
	// 09:30 уже был. По UTC-часам финал 09:30 UTC ещё впереди — решает зона.
	s.now = func() time.Time { return time.Date(2026, 11, 17, 4, 0, 0, 0, time.UTC) }
	mos := mustMeetingTZ(t, s, "Москва", 30, "mos@x.io", "2026-11-17", "Europe/Moscow")
	bMos, err := s.Book(mos.ID, "09:00", "Иван", "ivan@x.io")
	if err != nil {
		t.Fatal(err)
	}
	if s.IsPast(bMos) {
		t.Error("в 07:00 по Москве бронь 09:00 ещё в будущем")
	}
	s.now = func() time.Time { return time.Date(2026, 11, 17, 7, 0, 0, 0, time.UTC) }
	if !s.IsPast(bMos) {
		t.Error("в 10:00 по Москве бронь 09:00–09:30 завершена, вердикт — по часам организатора")
	}
	// Отмена завершившейся Записи отклоняется тоже по часам организатора:
	// по UTC финал 09:30 ещё «впереди», по Москве — уже был.
	if err := s.CancelBooking(bMos.ID, "ivan@x.io"); !errors.Is(err, ErrBookingPast) {
		t.Errorf("отмена завершившейся по Москве Записи: %v", err)
	}

	// Нью-Йорк (UTC-5). Финал 01:00 «ночной» брони по UTC уже прошёл к 04:00
	// UTC 17.11, но по часам организатора ещё 23:00 предыдущего дня — не прошло.
	s.now = func() time.Time { return time.Date(2026, 11, 16, 23, 0, 0, 0, time.UTC) }
	if err := s.SetWorkHours("ny@x.io", "2026-11-17", "00:00", "04:00"); err != nil {
		t.Fatal(err)
	}
	ny := mustMeetingTZ(t, s, "Нью-Йорк", 30, "ny@x.io", "2026-11-17", "America/New_York")
	bNy, err := s.Book(ny.ID, "00:30", "Оля", "olya@x.io")
	if err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return time.Date(2026, 11, 17, 4, 0, 0, 0, time.UTC) }
	if s.IsPast(bNy) {
		t.Error("по часам Нью-Йорка 00:30–01:00 наступит только в 06:00 UTC — не прошло")
	}
	if err := s.CancelBooking(bNy.ID, "olya@x.io"); err != nil {
		t.Errorf("грядущая по Нью-Йорку Запись должна отменяться: %v", err)
	}
}

// Запись на отменённую Встречу отклоняется различимой ошибкой
// (handler — 422): Встреча не в продаже, но её id Посетитель знать может.
func TestBook_CancelledMeetingRejected(t *testing.T) {
	s := newTestStore(t)
	m := mustMeeting(t, s, "Звонок", 30, "boss@x.io", "2026-11-17")
	mustCancelMeeting(t, s, m.ID)

	if _, err := s.Book(m.ID, "09:00", "Иван", "ivan@example.com"); !errors.Is(err, ErrMeetingCancelled) {
		t.Errorf("запись на отменённую Встречу: %v, хотим ErrMeetingCancelled", err)
	}
}

// Освобождённое Отменой Встречи время не мешает Организатору: пересекающийся
// Интервал его другой Встречи на ту же дату снова свободен (busyByOwner
// отфильтровывает отменённые Записи).
func TestCancelMeeting_FreesOwnerTime(t *testing.T) {
	s := newTestStore(t)
	m30 := mustMeeting(t, s, "Короткий колл", 30, "boss@x.io", "2026-11-17")
	m60 := mustMeeting(t, s, "Часовой разбор", 60, "boss@x.io", "2026-11-17")
	if _, err := s.Book(m30.ID, "10:30", "Иван", "ivan@example.com"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Book(m60.ID, "10:00", "Оля", "olya@example.com"); !errors.Is(err, ErrSlotTaken) {
		t.Fatalf("до Отмены 10:00–11:00 занят пересечением с 10:30–11:00: %v", err)
	}

	mustCancelMeeting(t, s, m30.ID)

	if _, err := s.Book(m60.ID, "10:00", "Оля", "olya@example.com"); err != nil {
		t.Errorf("после Отмены Встречи Интервал свободен в другой её встрече того же организатора: %v", err)
	}
	if statuses := freeStarts(t, s, m60.ID); statuses["10:00"] != SlotTaken {
		t.Errorf("свежая запись 10:00 в 60-мин сетке должна быть занята: %s", statuses["10:00"])
	}
}

// Мягкая Отмена Встречи переживает перезакрытие базы: статус Встречи и её
// Записей читается из файла, отменённая Встреча остаётся скрытой, а её
// освобождённое время — доступным.
func TestCancelMeeting_PersistsAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "calendar.db")
	fixed := func() time.Time { return time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC) }

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s.now = fixed
	m := mustMeeting(t, s, "Не переживёт стирание", 30, "boss@x.io", "2026-11-17")
	b, err := s.Book(m.ID, "09:00", "Иван", "ivan@example.com")
	if err != nil {
		t.Fatal(err)
	}
	mustCancelMeeting(t, s, m.ID)
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	s2, err := Open(path) // fresh соединение поверх WAL
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	s2.now = fixed

	if ms := s2.Meetings("boss@x.io"); len(ms) != 0 {
		t.Errorf("отменённая Встреча не должна возвращаться после рестарта: %+v", ms)
	}
	if mm, ok := s2.Meeting(m.ID); !ok || mm.State != MeetingCancelled {
		t.Errorf("статус Отмены Встречи должен лежать в файле: %+v ok=%v", mm, ok)
	}
	bs := s2.BookingsByUser("ivan@example.com")
	if len(bs) != 1 || bs[0].ID != b.ID || bs[0].State != BookingCancelled {
		t.Errorf("отменённая Запись не пережила рестарт: %+v", bs)
	}
	if _, err := s2.Book(m.ID, "09:00", "Оля", "olya@example.com"); !errors.Is(err, ErrMeetingCancelled) {
		t.Errorf("после рестарта запись отменённой Встречи должна отклоняться: %v", err)
	}
}

// Ядро итерации (ADR-0001): данные переживают перезапуск процесса —
// то же окно файла, новое соединение, миграции не повторяются.
func TestPersistAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "calendar.db")
	fixed := func() time.Time { return time.Date(2026, 11, 16, 12, 0, 0, 0, time.UTC) }

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s.now = fixed
	m := mustMeeting(t, s, "Переживает рестарт", 30, "boss@x.io", "2026-11-17")
	if err := s.SetWorkHours("boss@x.io", "2026-11-20", "10:00", "12:00"); err != nil {
		t.Fatal(err)
	}
	b, err := s.Book(m.ID, "10:00", "Иван", "ivan@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	s2, err := Open(path) // fresh соединение поверх WAL
	if err != nil {
		t.Fatal(err)
	}
	defer s2.Close()
	s2.now = fixed

	if ms := s2.Meetings("boss@x.io"); len(ms) != 1 || ms[0].ID != m.ID || ms[0].CreatedAt.IsZero() {
		t.Errorf("встреча не пережила рестарт: %+v", ms)
	}
	if h := s2.WorkHours("boss@x.io", "2026-11-20"); h.WorkStart != "10:00" || h.WorkEnd != "12:00" {
		t.Errorf("границы дня не пережили рестарт: %+v", h)
	}
	bs := s2.BookingsByUser("ivan@example.com")
	if len(bs) != 1 || bs[0].ID != b.ID || bs[0].End != "10:30" {
		t.Errorf("запись не пережила рестарт: %+v", bs)
	}
	// Конфликт после рестарта по-прежнему блокирует интервал.
	if _, err := s2.Book(m.ID, "10:00", "Оля", "olya@example.com"); !errors.Is(err, ErrSlotTaken) {
		t.Errorf("после рестарта ожидан 409-конфликт, получили %v", err)
	}
	var migrations int
	if err := s2.db.QueryRow(`SELECT count(*) FROM schema_version`).Scan(&migrations); err != nil {
		t.Fatal(err)
	}
	if migrations != 5 {
		t.Errorf("миграция должна примениться один раз, посчитано %d", migrations)
	}
}
