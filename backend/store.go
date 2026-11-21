package main

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // база часовых поясов в бинаре (ADR-0004): зоны работают без zoneinfo окружения

	_ "modernc.org/sqlite" // чистый Go, без cgo (ADR-0001)
)

// Hours — границы рабочего дня; хранятся для пары (email организатора, дата).
type Hours struct {
	WorkStart string `json:"workStart"` // "HH:MM"
	WorkEnd   string `json:"workEnd"`
}

func defaultHours() Hours { return Hours{WorkStart: "09:00", WorkEnd: "18:00"} }

// Meeting — Встреча, назначенная на конкретный день окна доступности.

func pubID(prefix string, n int64) string { return fmt.Sprintf("%s-%d", prefix, n) }

func parsePublicID(prefix, id string) (int64, bool) {
	rest := strings.TrimPrefix(id, prefix+"-")
	if rest == id {
		return 0, false
	}
	n, err := strconv.ParseInt(rest, 10, 64)
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

// rowScanner — *sql.Row и компании.
type rowScanner interface {
	Scan(dest ...any) error
}

// execer — общий интерфейс запросов вне и внутри транзакции; его
// удовлетворяют и *sql.DB, и *sql.Tx.
type execer interface {
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
}

// Meeting — Встреча, назначенная на конкретный день окна доступности.
type Meeting struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Description  string    `json:"description"`
	DurationMin  int       `json:"durationMin"`            // продолжительность Интервала
	StartStepMin *int      `json:"startStepMin,omitempty"` // Шаг стартов; nil = длительность (ADR-0003)
	OwnerEmail   string    `json:"ownerEmail"`             // email организатора: по нему проверяется занятость
	OwnerName    string    `json:"ownerName"`              // имя организатора, необязательное
	Date         string    `json:"date"`                   // "2006-01-02"
	State        string    `json:"state"`                  // "active" | "cancelled"
	Timezone     string    `json:"timezone"`               // IANA-зона Организатора из профиля; без профиля — "UTC"
	CreatedAt    time.Time `json:"createdAt"`
}

// startStep — Шаг стартов сетки: сохранённый, либо длительность, если не задан
// (ADR-0003: NULL = длительность, существующие Встречи сохраняют ту же сетку).
func (m *Meeting) startStep() int {
	if m.StartStepMin != nil {
		return *m.StartStepMin
	}
	return m.DurationMin
}

// Booking — Запись посетителя на Интервал Встречи.
type Booking struct {
	ID        string    `json:"id"`
	MeetingID string    `json:"meetingId"`
	Date      string    `json:"date"`
	Start     string    `json:"start"`
	End       string    `json:"end"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	State     string    `json:"state"` // "active" | "cancelled"
	CreatedAt time.Time `json:"createdAt"`
}

// Состояния Записи; cancelled — терминальный (Отмена Записи Посетителем).
const (
	BookingActive    = "active"
	BookingCancelled = "cancelled"
)

// Состояния Встречи; cancelled — терминальный (Отмена Встречи Организатором).
const (
	MeetingActive    = "active"
	MeetingCancelled = "cancelled"
)

type DayAvailability struct {
	Date string `json:"date"`
	Free int    `json:"free"` // число свободных Интервалов
}

// Ошибки различимы на уровне HTTP-кодов.
var (
	ErrMeetingNotFound      = fmt.Errorf("встреча не найдена")
	ErrSlotUnavailable      = fmt.Errorf("интервал недоступен (серый или прошедший)")
	ErrSlotTaken            = fmt.Errorf("интервал занят в этой или другой встрече организатора")
	ErrNameRequired         = fmt.Errorf("укажите имя")
	ErrEmailRequired        = fmt.Errorf("укажите email")
	ErrOwnerRequired        = fmt.Errorf("укажите email организатора")
	ErrDateRequired         = fmt.Errorf("дата встречи обязательна и должна быть в окне доступности")
	ErrOutsideWindow        = fmt.Errorf("дата вне окна доступности")
	ErrBookingNotFound      = fmt.Errorf("запись не найдена")
	ErrBookingEmailMismatch = fmt.Errorf("отменить запись может только её посетитель: email не совпадает")
	ErrBookingPast          = fmt.Errorf("запись уже завершилась — отменить её нельзя")
	ErrMeetingCancelled     = fmt.Errorf("встреча отменена организатором")
	ErrStartStepRange       = fmt.Errorf("шаг стартов должен быть целым от 1 до длительности включительно")
)

const (
	// meetings + профиль Организатора: зона резолвится на чтении (ADR-0004),
	// без профиля — «UTC» (детерминированный дефолт, не локаль процесса).
	meetingCols       = `m.id, m.name, m.description, m.duration_min, m.start_step_min, m.owner_email, m.owner_name, m.date, m.state, m.created_at, COALESCE(o.timezone, 'UTC')`
	meetingFromClause = `FROM meetings m LEFT JOIN organizers o ON o.email = m.owner_email`
	bookingCols       = `id, meeting_id, date, start, end, name, email, state, created_at`
)

// Store — хранилище на SQLite (ADR-0001). Публичные методы — тот же
// интерфейс, что был у in-memory Store: handler'ы и тесты не изменились.
type Store struct {
	db  *sql.DB
	now func() time.Time // подменяется в тестах
}

// Open открывает (или создаёт) базу и прогоняет неприменённые миграции.
// Одно писательское соединение (MaxOpenConns(1)): SQLite пишет
// последовательно, проверяющий-then-insert в Book не требует внешних локов.
func Open(path string) (*Store, error) {
	dsn := path
	if !strings.Contains(path, "?") {
		dsn = path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		return nil, err
	}
	if err := migrateDB(db); err != nil {
		return nil, err
	}
	return &Store{db: db, now: time.Now}, nil
}

// NewStore — изолированная in-memory база для тестов: та же схема и то же
// поведение, ничего не переживает процесс.
func NewStore() *Store {
	s, err := Open(":memory:?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		panic(err)
	}
	return s
}

// Close освобождает соединение.
func (s *Store) Close() error { return s.db.Close() }

// --- Часовой пояс Организатора (ADR-0004) ---

// defaultZone — имя fallback-зоны, когда профиля Организатора нет:
// детерминированный UTC, а не локаль процесса. Единственное место,
// где Go-литерал «UTC» задан; SQL COALESCE(..., 'UTC') в meetingCols —
// тот же UTC на чтении Встреч.
const defaultZone = "UTC"

// zoneNameVia — IANA-имя зоны из профиля Организатора; без профиля — «UTC».
// Все вычисления времени (календарные дни, Окно доступности, вердикт
// «уже прошло») идут в этой зоне, а не в локации процесса.
func zoneNameVia(e execer, ownerEmail string) string {
	if ownerEmail == "" {
		return defaultZone
	}
	var tz string
	if err := e.QueryRow(`SELECT timezone FROM organizers WHERE email = ?`, ownerEmail).Scan(&tz); err != nil {
		return defaultZone
	}
	return tz
}

// zoneVia — Location по профилю; некорректное значение в базе не ломает
// детерминизм — фолбэк UTC (создание с такой зоной отклоняется заранее).
func zoneVia(e execer, ownerEmail string) *time.Location {
	loc, err := time.LoadLocation(zoneNameVia(e, ownerEmail))
	if err != nil {
		return time.UTC
	}
	return loc
}

// resolveZone — зона Организатора по email (вне транзакции).
func (s *Store) resolveZone(ownerEmail string) *time.Location { return zoneVia(s.db, ownerEmail) }

// OrganizerZone — IANA-имя зоны из профиля Организатора; без профиля — «UTC».
// Для ответов, где зона объясняет, в каких часах стоят границы рабочего дня.
func (s *Store) OrganizerZone(ownerEmail string) string { return zoneNameVia(s.db, ownerEmail) }

// previewZone — зона превью ещё не сохранённой Встречи: переданная >
// сохранённый профиль > UTC. Превью и будущее сохранение должны считать
// «сегодня» и «прошло» одними часами, иначе у границы полуночи превью
// разъедется с реальностью. Некорректная переданная — как непереданная.
func (s *Store) previewZone(ownerEmail, fallbackZone string) *time.Location {
	if name := strings.TrimSpace(fallbackZone); name != "" {
		if loc, err := time.LoadLocation(name); err == nil {
			return loc
		}
	}
	return s.resolveZone(ownerEmail)
}

func (s *Store) todayIn(zone *time.Location) string { return s.now().In(zone).Format("2006-01-02") }

func (s *Store) windowLastIn(zone *time.Location) string {
	base, _ := time.Parse("2006-01-02", s.todayIn(zone))
	return base.AddDate(0, 0, windowDays-1).Format("2006-01-02")
}

func (s *Store) inWindow(date string, zone *time.Location) bool {
	if _, err := time.Parse("2006-01-02", date); err != nil {
		return false
	}
	today := s.todayIn(zone)
	return date >= today && date <= s.windowLastIn(zone)
}

// dayNowMin — «сейчас» в минутах для выбранного дня по часам зоны; дни
// в будущем и прошлом получают DayInFuture / DayInPast.
func (s *Store) dayNowMin(zone *time.Location, date string) int {
	now := s.now().In(zone)
	today := now.Format("2006-01-02")
	switch {
	case date > today:
		return DayInFuture
	case date < today:
		return DayInPast
	default:
		return now.Hour()*60 + now.Minute()
	}
}

// --- Рабочий день: пара (email, дата) ---

func workHoursVia(e execer, owner, date string) Hours {
	var st, en string
	if err := e.QueryRow(
		`SELECT work_start, work_end FROM work_hours WHERE owner_email = ? AND date = ?`,
		owner, date).Scan(&st, &en); err != nil {
		return defaultHours()
	}
	return Hours{WorkStart: st, WorkEnd: en}
}

// WorkHours возвращает границы дня для пары (owner, date); если пара не
// сохранялась — дефолт 09:00–18:00.
func (s *Store) WorkHours(owner, date string) Hours { return workHoursVia(s.db, owner, date) }

// SetWorkHours сохраняет границы только для этой даты этого организатора.
func (s *Store) SetWorkHours(owner, date, workStart, workEnd string) error {
	if !ValidEmail(owner) {
		return ErrOwnerRequired
	}
	if _, err := time.Parse("2006-01-02", date); err != nil {
		return fmt.Errorf("некорректная дата: %s", date)
	}
	st, ok1 := ParseHM(workStart)
	en, ok2 := ParseHM(workEnd)
	if !ok1 || !ok2 || en <= st {
		return fmt.Errorf("некорректные границы рабочего дня: %s / %s", workStart, workEnd)
	}
	_, err := s.db.Exec(`INSERT INTO work_hours (owner_email, date, work_start, work_end)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(owner_email, date)
		DO UPDATE SET work_start = excluded.work_start, work_end = excluded.work_end`,
		owner, date, FormatHM(st), FormatHM(en))
	return err
}

// --- Встречи ---

func meetingFrom(row rowScanner) (*Meeting, bool) {
	var (
		id         int64
		name, desc string
		dur        int
		step       sql.NullInt64
		oe, on     string
		date       string
		state      string
		created    string
		tz         string
	)
	if err := row.Scan(&id, &name, &desc, &dur, &step, &oe, &on, &date, &state, &created, &tz); err != nil {
		return nil, false
	}
	t, _ := time.Parse(time.RFC3339Nano, created)
	m := &Meeting{ID: pubID("m", id), Name: name, Description: desc, DurationMin: dur,
		OwnerEmail: oe, OwnerName: on, Date: date, State: state, Timezone: tz, CreatedAt: t}
	if step.Valid {
		sm := int(step.Int64)
		m.StartStepMin = &sm
	}
	return m, true
}

func meetingByID(e execer, n int64) (*Meeting, bool) {
	return meetingFrom(e.QueryRow(`SELECT `+meetingCols+` `+meetingFromClause+` WHERE m.id = ?`, n))
}

// CreateMeeting создаёт Встречу, назначенную на конкретный день окна.
// startStepMin == nil — Шаг стартов равен длительности (ADR-0003); иначе
// принимаются только целые 1..durationMin. Непустой timezone — upsert профиля
// Организатора (пустой — профиль не трогает); «сегодня» и Окно доступности
// валидируются по часам зоны (ADR-0004).
func (s *Store) CreateMeeting(name, description string, durationMin int, startStepMin *int, ownerEmail, ownerName, date, timezone string) (*Meeting, error) {
	if name == "" {
		return nil, fmt.Errorf("название обязательно")
	}
	if !ValidEmail(ownerEmail) {
		return nil, ErrOwnerRequired
	}
	if durationMin <= 0 || durationMin > 24*60 {
		return nil, fmt.Errorf("некорректная продолжительность: %d", durationMin)
	}
	if startStepMin != nil && (*startStepMin < 1 || *startStepMin > durationMin) {
		return nil, ErrStartStepRange
	}
	timezone = strings.TrimSpace(timezone)
	if timezone != "" {
		if _, err := time.LoadLocation(timezone); err != nil {
			return nil, fmt.Errorf("некорректный часовой пояс: %s", timezone)
		}
	}
	createdAt := s.now().UTC().Format(time.RFC3339Nano)
	var stepArg any
	if startStepMin != nil {
		stepArg = *startStepMin
	}

	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	if timezone != "" {
		if _, err := tx.Exec(`INSERT INTO organizers (email, timezone) VALUES (?, ?)
			ON CONFLICT(email) DO UPDATE SET timezone = excluded.timezone`,
			ownerEmail, timezone); err != nil {
			return nil, err
		}
	}
	// Дата проверяется в зоне профиля — уже обновлённого этим запросом.
	if !s.inWindow(date, zoneVia(tx, ownerEmail)) {
		return nil, ErrDateRequired
	}
	res, err := tx.Exec(`INSERT INTO meetings (name, description, duration_min, start_step_min, owner_email, owner_name, date, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		name, description, durationMin, stepArg, ownerEmail, ownerName, date, createdAt)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	tzName := zoneNameVia(tx, ownerEmail)
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	t, _ := time.Parse(time.RFC3339Nano, createdAt)
	return &Meeting{ID: pubID("m", id), Name: name, Description: description, DurationMin: durationMin,
		StartStepMin: startStepMin, OwnerEmail: ownerEmail, OwnerName: ownerName, Date: date,
		State: MeetingActive, Timezone: tzName, CreatedAt: t}, nil
}

// Meetings возвращает доступные Встречи — все (для посетителя) или одного
// организатора, если owner непустой; отменённые исключены и там, и там.
// Колонки email — COLLATE NOCASE, регистр не значим.
func (s *Store) Meetings(owner string) []*Meeting {
	q := `SELECT ` + meetingCols + ` ` + meetingFromClause + ` WHERE m.state = ?`
	args := []any{MeetingActive}
	if owner != "" {
		q += ` AND m.owner_email = ?`
		args = append(args, owner)
	}
	q += ` ORDER BY m.date, m.created_at, m.id`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []*Meeting{}
	for rows.Next() {
		// Единый разбор колонок — тот же row-хелпер, что и для одной Встречи.
		m, ok := meetingFrom(rows)
		if !ok {
			return out
		}
		out = append(out, m)
	}
	return out
}

func (s *Store) Meeting(id string) (*Meeting, bool) {
	n, ok := parsePublicID("m", id)
	if !ok {
		return nil, false
	}
	return meetingByID(s.db, n)
}

// DeleteMeeting — Отмена Встречи (GLOSSARY: «Отмена Встречи»), мягкая:
// Встреча не стирается, а переходит в MeetingCancelled, и в той же транзакции
// её активные Записи становятся «Отменена» — Посетитель видит отмену в
// истории. Несуществующая (или некорректная) Встреча — ErrMeetingNotFound
// (handler — 404); повторная отмена уже отменённой — успешный no-op
// (handler — тот же 204). ON DELETE CASCADE в схеме остаётся непричастным:
// физически встречи больше не удаляются.
func (s *Store) DeleteMeeting(id string) error {
	n, ok := parsePublicID("m", id)
	if !ok {
		return ErrMeetingNotFound
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// Существование проверяем в той же транзакции, что и переход в cancelled.
	var state string
	err = tx.QueryRow(`SELECT state FROM meetings WHERE id = ?`, n).Scan(&state)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrMeetingNotFound
		}
		return err
	}
	if state == MeetingCancelled {
		return nil // повторная отмена идемпотентна
	}
	if _, err := tx.Exec(`UPDATE meetings SET state = ? WHERE id = ?`, MeetingCancelled, n); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE bookings SET state = ? WHERE meeting_id = ? AND state = ?`,
		BookingCancelled, n, BookingActive); err != nil {
		return err
	}
	return tx.Commit()
}

// --- Занятость организатора ---

// busyByOwner — занятые отрезки во всех Встречах организатора на диапазон
// дат. Конфликт — пересечение отрезков, поэтому сетки разной
// продолжительности сопоставляются корректно. Отменённые Записи время
// не занимают.
func busyByOwner(e execer, ownerEmail, dateFrom, dateTo string) map[string][]Interval {
	out := map[string][]Interval{}
	rows, err := e.Query(`SELECT b.date, b.start, b.end FROM bookings b
		JOIN meetings m ON m.id = b.meeting_id
		WHERE m.owner_email = ? AND b.date >= ? AND b.date <= ?
		AND b.state = ?`,
		ownerEmail, dateFrom, dateTo, BookingActive)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var date, st, en string
		if err := rows.Scan(&date, &st, &en); err != nil {
			return out
		}
		stMin, ok1 := ParseHM(st)
		enMin, ok2 := ParseHM(en)
		if ok1 && ok2 {
			out[date] = append(out[date], Interval{Start: stMin, End: enMin})
		}
	}
	return out
}

// --- Интервалы дня ---

// DaySlots — сетка Интервалов Встречи на её день: границы рабочего дня для
// (организатор, дата), сквозная занятость по всем Встречам организатора.
// Отменённая Встреча «не в продаже»: сетка недоступна (handler — 404).
func (s *Store) DaySlots(meetingID string) ([]Slot, bool) {
	m, ok := s.Meeting(meetingID)
	if !ok || m.State != MeetingActive {
		return nil, false
	}
	h := s.WorkHours(m.OwnerEmail, m.Date)
	zone := s.resolveZone(m.OwnerEmail)
	busy := busyByOwner(s.db, m.OwnerEmail, m.Date, m.Date)[m.Date]
	return GenerateSlots(h.WorkStart, h.WorkEnd, m.DurationMin, m.startStep(), s.dayNowMin(zone, m.Date), busy), true
}

// PreviewSlots — превью сетки дня для ещё не созданной Встречи (Сценарий 1:
// организатор видит доступность, конкретный Интервал не выбирает).
// Если owner задан, учитываются его уже забронированные Встречи.
// stepMin <= 0 — шаг равен длительности; fallbackZone — зона превью
// (переданная > сохранённый профиль > UTC).
func (s *Store) PreviewSlots(ownerEmail, date string, durationMin, stepMin int, fallbackZone string) []Slot {
	h := s.WorkHours(ownerEmail, date)
	zone := s.previewZone(ownerEmail, fallbackZone)
	var busy []Interval
	if ownerEmail != "" {
		busy = busyByOwner(s.db, ownerEmail, date, date)[date]
	}
	return GenerateSlots(h.WorkStart, h.WorkEnd, durationMin, stepMin, s.dayNowMin(zone, date), busy)
}

// MeetingAvailability — число свободных Интервалов на день Встречи
// (для карточек «Свободно»).
func (s *Store) MeetingAvailability(meetingID string) (DayAvailability, bool) {
	slots, ok := s.DaySlots(meetingID)
	if !ok {
		return DayAvailability{}, false
	}
	m, _ := s.Meeting(meetingID)
	return DayAvailability{Date: m.Date, Free: FreeCount(slots)}, true
}

// PreviewAvailability — окно доступности для создаваемой Встречи: каждый
// день считается по своим границам этого организатора. stepMin <= 0 — шаг
// равен длительности; fallbackZone — зона превью (переданная > сохранённый
// профиль > UTC).
func (s *Store) PreviewAvailability(durationMin, stepMin int, ownerEmail, fallbackZone string) []DayAvailability {
	zone := s.previewZone(ownerEmail, fallbackZone)
	out := make([]DayAvailability, 0, windowDays)
	base, _ := time.Parse("2006-01-02", s.todayIn(zone))
	var allBusy map[string][]Interval
	if ownerEmail != "" {
		allBusy = busyByOwner(s.db, ownerEmail, s.todayIn(zone), s.windowLastIn(zone))
	}
	for i := 0; i < windowDays; i++ {
		date := base.AddDate(0, 0, i).Format("2006-01-02")
		h := s.WorkHours(ownerEmail, date)
		var busy []Interval
		if allBusy != nil {
			busy = allBusy[date]
		}
		slots := GenerateSlots(h.WorkStart, h.WorkEnd, durationMin, stepMin, s.dayNowMin(zone, date), busy)
		out = append(out, DayAvailability{Date: date, Free: FreeCount(slots)})
	}
	return out
}

// --- Бронирование ---

// Book занимает Интервал Встречи; день берётся из Встречи. Проверка
// конфликта и вставка — в одной транзакции: два параллельных бронирования
// одного Интервала не могут оба получить 201 (SQLite сериализуетWriter'ы).
func (s *Store) Book(meetingID, start, name, email string) (*Booking, error) {
	if name == "" {
		return nil, ErrNameRequired
	}
	if !ValidEmail(email) {
		return nil, ErrEmailRequired
	}
	mn, ok := parsePublicID("m", meetingID)
	if !ok {
		return nil, ErrMeetingNotFound
	}

	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	m, ok := meetingByID(tx, mn)
	if !ok {
		return nil, ErrMeetingNotFound
	}
	// Отменённая Встреча «не в продаже»: запись отклоняется различимой ошибкой.
	if m.State == MeetingCancelled {
		return nil, ErrMeetingCancelled
	}
	// «Сегодня», окно и «прошло» — по часам организатора Встречи (ADR-0004).
	zone := zoneVia(tx, m.OwnerEmail)
	if m.Date < s.todayIn(zone) || m.Date > s.windowLastIn(zone) {
		return nil, ErrOutsideWindow
	}

	h := workHoursVia(tx, m.OwnerEmail, m.Date)
	busy := busyByOwner(tx, m.OwnerEmail, m.Date, m.Date)[m.Date]
	slots := GenerateSlots(h.WorkStart, h.WorkEnd, m.DurationMin, m.startStep(), s.dayNowMin(zone, m.Date), busy)
	slot, allowed := SlotAllowed(slots, start)
	if !allowed {
		if slot.Status == SlotTaken {
			return nil, ErrSlotTaken
		}
		return nil, ErrSlotUnavailable
	}

	createdAt := s.now().UTC().Format(time.RFC3339Nano)
	res, err := tx.Exec(`INSERT INTO bookings (meeting_id, date, start, end, name, email, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		mn, m.Date, start, slot.End, name, email, createdAt)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	t, _ := time.Parse(time.RFC3339Nano, createdAt)
	return &Booking{ID: pubID("b", id), MeetingID: m.ID, Date: m.Date, Start: start, End: slot.End,
		Name: name, Email: email, State: BookingActive, CreatedAt: t}, nil
}

// BookingsByUser — все Записи посетителя по email, отсортированы по времени.
func (s *Store) BookingsByUser(email string) []*Booking {
	rows, err := s.db.Query(`SELECT `+bookingCols+` FROM bookings WHERE email = ?
		ORDER BY date, start, id`, email)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []*Booking{}
	for rows.Next() {
		var (
			id        int64
			meetingID int64
			date, st  string
			en        string
			nm        string
			em        string
			state     string
			created   string
		)
		if err := rows.Scan(&id, &meetingID, &date, &st, &en, &nm, &em, &state, &created); err != nil {
			return out
		}
		t, _ := time.Parse(time.RFC3339Nano, created)
		out = append(out, &Booking{ID: pubID("b", id), MeetingID: pubID("m", meetingID),
			Date: date, Start: st, End: en, Name: nm, Email: em, State: state, CreatedAt: t})
	}
	return out
}

// CancelBooking — Отмена Записи Посетителем: Запись переходит в терминальный
// «cancelled», её Интервал мгновенно освобождается. Переход — в транзакции
// (как Book): проверка и запись атомарны.
func (s *Store) CancelBooking(bookingID, email string) error {
	n, ok := parsePublicID("b", bookingID)
	if !ok {
		return ErrBookingNotFound
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var state, visitorEmail, date, end, ownerEmail string
	err = tx.QueryRow(`SELECT b.state, b.email, b.date, b.end, m.owner_email
		FROM bookings b JOIN meetings m ON m.id = b.meeting_id WHERE b.id = ?`, n).
		Scan(&state, &visitorEmail, &date, &end, &ownerEmail)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrBookingNotFound
		}
		return err
	}
	// Колонка email — COLLATE NOCASE; сверка самозаявленной идентичности
	// без учёта регистра (GLOSSARY: Посетитель). Отклоняем и для уже
	// отменённой Записи: отменить её мог только её посетитель.
	if !strings.EqualFold(strings.TrimSpace(visitorEmail), strings.TrimSpace(email)) {
		return ErrBookingEmailMismatch
	}
	if state == BookingCancelled {
		return nil // повторная отмена — успешный no-op (идемпотентность)
	}
	// Отменить можно только предстоящую Запись (GLOSSARY: Запись); вердикт
	// — по часам Организатора Встречи (ADR-0004).
	if s.pastIn(zoneVia(tx, ownerEmail), date, end) {
		return ErrBookingPast
	}
	if _, err := tx.Exec(`UPDATE bookings SET state = ? WHERE id = ?`, BookingCancelled, n); err != nil {
		return err
	}
	return tx.Commit()
}

// IsPast — Встреча закончилась (автоперенос Записи в группу «Прошедшие»).
// Wall-clock окончание понимается в зоне Организатора Встречи (ADR-0004);
// Запись без различимой Встречи — UTC.
func (s *Store) IsPast(b *Booking) bool {
	zone := time.UTC
	if m, ok := s.Meeting(b.MeetingID); ok {
		zone = s.resolveZone(m.OwnerEmail)
	}
	return s.pastIn(zone, b.Date, b.End)
}

// pastIn — завершена ли Запись (date + "HH:MM" окончание) на текущий момент
// по часам зоны.
func (s *Store) pastIn(zone *time.Location, date, end string) bool {
	now := s.now().In(zone)
	endMin, ok := ParseHM(end)
	if !ok {
		return true
	}
	d, err := time.Parse("2006-01-02", date)
	if err != nil {
		return true
	}
	e := time.Date(d.Year(), d.Month(), d.Day(), 0, endMin, 0, 0, zone)
	return !e.After(now)
}
