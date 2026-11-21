package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

type server struct {
	store *Store
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

// writeErrInternal — неизвестная внутренняя ошибка: в лог уходит оригинал,
// клиенту — общий 500 без деталей.
func writeErrInternal(w http.ResponseWriter, err error) {
	log.Printf("internal error: %v", err)
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "внутренняя ошибка сервера"})
}

// bookingView — запись в представлении для клиента: встреча + флаг «пройдена»
// + данные организатора (имя и/или email для вкладки «Мои записи») + состояние
// Встречи (источник «Отменена» выводится на клиенте, не хранится) + её
// часовая зона (часы start/end стоят по часам организатора, ADR-0004).
type bookingView struct {
	*Booking
	MeetingName       string `json:"meetingName"`
	MeetingOwnerName  string `json:"meetingOwnerName"`
	MeetingOwnerEmail string `json:"meetingOwnerEmail"`
	MeetingState      string `json:"meetingState"`
	Past              bool   `json:"past"`
	Timezone          string `json:"timezone"`
}

type hoursView struct {
	Owner string `json:"owner"`
	Date  string `json:"date"`
	// Timezone — зона профиля Организатора (дефолт UTC): в ней понимаются
	// границы рабочего дня на дату.
	Timezone string `json:"timezone"`
	Hours
}

// /api/workhours?owner=boss@x.io&date=2026-11-20 — границы рабочего дня
// конкретного организатора на конкретную дату (дефолт 09:00–18:00) и его
// IANA-зона (профиль > UTC) — в каких часах эти границы стоят.
// PUT с телом {owner, date, workStart, workEnd} сохраняет границы только
// для этой пары; страница создания при этом не «перезагружается» —
// фронтенд обновляет превью на месте.
func (srv *server) handleWorkhours(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		owner := r.URL.Query().Get("owner")
		date := r.URL.Query().Get("date")
		if !ValidEmail(owner) || date == "" {
			writeErr(w, http.StatusBadRequest, ErrOwnerRequired)
			return
		}
		writeJSON(w, http.StatusOK, hoursView{
			Owner: owner, Date: date,
			Timezone: srv.store.OrganizerZone(owner),
			Hours:    srv.store.WorkHours(owner, date),
		})
	case http.MethodPut:
		var req hoursView
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		if err := srv.store.SetWorkHours(req.Owner, req.Date, req.WorkStart, req.WorkEnd); err != nil {
			writeErr(w, http.StatusUnprocessableEntity, err)
			return
		}
		writeJSON(w, http.StatusOK, hoursView{
			Owner: req.Owner, Date: req.Date,
			Timezone: srv.store.OrganizerZone(req.Owner),
			Hours:    srv.store.WorkHours(req.Owner, req.Date),
		})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (srv *server) handleMeetings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		// ?owner=boss@x.io — только встречи организатора (Сценарий 1),
		// без параметра — все доступные встречи (Сценарий 2).
		owner := r.URL.Query().Get("owner")
		if owner != "" && !ValidEmail(owner) {
			writeErr(w, http.StatusBadRequest, ErrOwnerRequired)
			return
		}
		writeJSON(w, http.StatusOK, srv.store.Meetings(owner))
	case http.MethodPost:
		var m struct {
			Name         string `json:"name"`
			Description  string `json:"description"`
			DurationMin  int    `json:"durationMin"`
			StartStepMin *int   `json:"startStepMin"`
			OwnerEmail   string `json:"ownerEmail"`
			OwnerName    string `json:"ownerName"`
			Date         string `json:"date"`
			Timezone     string `json:"timezone"`
		}
		if err := json.NewDecoder(r.Body).Decode(&m); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		created, err := srv.store.CreateMeeting(m.Name, m.Description, m.DurationMin, m.StartStepMin, m.OwnerEmail, m.OwnerName, m.Date, m.Timezone)
		if err != nil {
			writeErr(w, http.StatusUnprocessableEntity, err)
			return
		}
		writeJSON(w, http.StatusCreated, created)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// /api/meetings/{id} — DELETE (Отмена Встречи); /api/meetings/{id}/... — остальное.
func (srv *server) handleMeeting(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	sub := r.PathValue("sub")
	if sub == "" {
		// DELETE /api/meetings/{id} — Отмена Встречи. Зарегистрирован только
		// шаблон с методом — прочие методы отсекает сам mux (405).
		if err := srv.store.DeleteMeeting(id); err != nil {
			if errors.Is(err, ErrMeetingNotFound) {
				writeErr(w, http.StatusNotFound, err)
				return
			}
			writeErrInternal(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}

	switch {
	case r.Method == http.MethodGet && sub == "availability":
		av, ok := srv.store.MeetingAvailability(id)
		if !ok {
			writeErr(w, http.StatusNotFound, ErrMeetingNotFound)
			return
		}
		writeJSON(w, http.StatusOK, av)

	case r.Method == http.MethodGet && sub == "slots":
		// День встречи фиксирован — параметр date не нужен.
		slots, ok := srv.store.DaySlots(id)
		if !ok {
			writeErr(w, http.StatusNotFound, ErrMeetingNotFound)
			return
		}
		writeJSON(w, http.StatusOK, slots)

	case r.Method == http.MethodPost && sub == "book":
		var req struct{ Start, Name, Email string }
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		b, err := srv.store.Book(id, req.Start, req.Name, req.Email)
		switch {
		case errors.Is(err, ErrSlotTaken):
			writeErr(w, http.StatusConflict, err)
		case err != nil:
			writeErr(w, http.StatusUnprocessableEntity, err)
		default:
			writeJSON(w, http.StatusCreated, b)
		}

	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

// /api/preview/availability?duration=30&step=15&owner=...&timezone=... и
// /api/preview/slots?date=&duration=30&step=15&owner=...&timezone=... — превью
// сетки для ещё не созданной встречи (Сценарий 1: организатор видит доступные
// интервалы, конкретный интервал он не выбирает). Шаг стартов необязателен:
// без него старты идут через длительность. Конвенция кодов как при создании:
// нечисловой step — 400 (синтаксис query), значение вне 1..duration —
// 422 ErrStartStepRange (доменное правило).
func (srv *server) handlePreview(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	dur, err := strconv.Atoi(q.Get("duration"))
	if err != nil || dur <= 0 {
		writeErr(w, http.StatusBadRequest, errors.New("некорректный параметр duration"))
		return
	}
	step := 0
	if raw := q.Get("step"); raw != "" {
		step, err = strconv.Atoi(raw)
		if err != nil {
			writeErr(w, http.StatusBadRequest, errors.New("некорректный параметр step: ожидается целое число"))
			return
		}
		if step < 1 || step > dur {
			writeErr(w, http.StatusUnprocessableEntity, ErrStartStepRange)
			return
		}
	}
	owner := q.Get("owner")
	if owner != "" && !ValidEmail(owner) {
		writeErr(w, http.StatusBadRequest, ErrOwnerRequired)
		return
	}
	// Необязательная IANA-зона: превью до первого сохранения считается по
	// часам поля «Часовой пояс» (переданная > сохранённый профиль > UTC).
	tz := q.Get("timezone")
	if r.PathValue("sub") == "availability" {
		writeJSON(w, http.StatusOK, srv.store.PreviewAvailability(dur, step, owner, tz))
		return
	}
	date := q.Get("date")
	if date == "" {
		writeErr(w, http.StatusBadRequest, errors.New("не указан параметр date"))
		return
	}
	writeJSON(w, http.StatusOK, srv.store.PreviewSlots(owner, date, dur, step, tz))
}

// /api/bookings?email=... — все записи пользователя, прошедшие помечены past.
func (srv *server) handleBookings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	email := r.URL.Query().Get("email")
	if email == "" {
		writeErr(w, http.StatusBadRequest, errors.New("не указан параметр email"))
		return
	}
	views := []bookingView{}
	for _, b := range srv.store.BookingsByUser(email) {
		// Встреча может быть уже без различимого профиля — зону даёт
		// SQL-джойн с COALESCE 'UTC'; нет и Встречи — тот же defaultZone.
		view := bookingView{Booking: b, Past: srv.store.IsPast(b), MeetingState: MeetingActive, Timezone: defaultZone}
		if m, ok := srv.store.Meeting(b.MeetingID); ok {
			view.MeetingName = m.Name
			view.MeetingOwnerName = m.OwnerName
			view.MeetingOwnerEmail = m.OwnerEmail
			view.MeetingState = m.State
			view.Timezone = m.Timezone
		}
		views = append(views, view)
	}
	writeJSON(w, http.StatusOK, views)
}

// /api/bookings/{id}?email=... — DELETE: Отмена Записи Посетителем.
// 204 — отменена (или уже отменена: идемпотентность), 404 — нет такой,
// 409 — email не совпадает, 422 — запись уже завершилась, 500 — прочее.
func (srv *server) handleCancelBooking(w http.ResponseWriter, r *http.Request) {
	switch err := srv.store.CancelBooking(r.PathValue("id"), r.URL.Query().Get("email")); {
	case errors.Is(err, ErrBookingNotFound):
		writeErr(w, http.StatusNotFound, err)
	case errors.Is(err, ErrBookingEmailMismatch):
		writeErr(w, http.StatusConflict, err)
	case errors.Is(err, ErrBookingPast):
		writeErr(w, http.StatusUnprocessableEntity, err)
	case err != nil:
		writeErrInternal(w, err)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// newMux — регистрация всех маршрутов API на http.ServeMux. Вынесена из
// main, чтобы e2e (ADR-0005) поднимал тот же роутер поверх httptest.Server;
// поведение боевого процесса не меняется.
func newMux(store *Store) *http.ServeMux {
	srv := &server{store: store}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/workhours", srv.handleWorkhours)
	mux.HandleFunc("/api/meetings", srv.handleMeetings)
	mux.HandleFunc("DELETE /api/meetings/{id}", srv.handleMeeting)
	mux.HandleFunc("/api/meetings/{id}/{sub}", srv.handleMeeting)
	mux.HandleFunc("/api/preview/{sub}", srv.handlePreview)
	mux.HandleFunc("/api/bookings", srv.handleBookings)
	mux.HandleFunc("DELETE /api/bookings/{id}", srv.handleCancelBooking)
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	// SPA (go:embed web/dist): частные /api-маршруты всегда специфичнее
	// этого catch-all, конфликтов нет.
	mux.Handle("/", staticHandler())
	return mux
}

// listenAddr: явный ADDR > PORT (convention хостингов, в т.ч. Render) > :8080.
func listenAddr() string {
	if addr := os.Getenv("ADDR"); addr != "" {
		return addr
	}
	if port := os.Getenv("PORT"); port != "" {
		return ":" + port
	}
	return ":8080"
}

func main() {
	addr := listenAddr()
	store, dbPath, err := openStore()
	if err != nil {
		log.Fatal(err)
	}

	// Демо-данные только при пустой базе, иначе персистентный календарь
	// каждый рестарт плодил бы новые «Демо-звонки».
	demo, _ := strconv.Atoi(os.Getenv("DEMO_DAYS_AHEAD"))
	if demo <= 0 {
		demo = 2
	}
	if len(store.Meetings("")) == 0 {
		demoDate := time.Now().AddDate(0, 0, demo).Format("2006-01-02")
		// Пустая зона: профиль Организатора для демо не заводим —
		// встречи живут детерминированным UTC-дефолтом.
		if _, err := store.CreateMeeting("Демо-звонок", "Пример встречи для пилота: 30-минутные интервалы", 30, nil, "demo@calendar.app", "Демо-организатор", demoDate, ""); err != nil {
			log.Fatal(err)
		}
	}

	log.Printf("calendar api on http://localhost%s (db: %s)", addr, dbPath)
	log.Fatal(http.ListenAndServe(addr, newMux(store)))
}

// openStore — персистентная база из env DATA_PATH; каталог создаём,
// SQLite сам файл сделает.
func openStore() (*Store, string, error) {
	path := os.Getenv("DATA_PATH")
	if path == "" {
		path = "data/calendar.db"
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, "", err
		}
	}
	store, err := Open(path)
	if err != nil {
		return nil, "", err
	}
	return store, path, nil
}
