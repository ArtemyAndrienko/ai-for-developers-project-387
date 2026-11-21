package main

// HTTP e2e (ADR-0005): реальный роутер поверх httptest.Server и настоящая
// SQLite во временном каталоге. Покрывает ровно то, что не выражается на
// Store-шовне: полный happy-цикл через HTTP JSON, параллельную гонку
// бронирования (контрактная гарантия «ровно одна Запись succeeds») и
// конвенцию HTTP-кодов для шага превью (400 — синтаксис, 422 — домен).

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// e2eServer — httptest.Server поверх newMux с файловой SQLite в t.TempDir():
// тот же путь открытия (WAL + busy_timeout), что у боевого процесса.
func e2eServer(t *testing.T) *httptest.Server {
	t.Helper()
	store, err := Open(filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	srv := httptest.NewServer(newMux(store))
	t.Cleanup(srv.Close)
	return srv
}

// e2eRequest — базовый вызов без t.Fatalf: пригоден внутри goroutine
// (в гонке). Возвращает код ответа и сырое тело.
func e2eRequest(srv *httptest.Server, method, path string, body any) (int, []byte, error) {
	var rd io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return 0, nil, err
		}
		rd = bytes.NewReader(buf)
	}
	req, err := http.NewRequest(method, srv.URL+path, rd)
	if err != nil {
		return 0, nil, err
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	return resp.StatusCode, raw, err
}

// e2eDo — e2eRequest с фатальными проверками транспорта и декодирования
// JSON (вызывается только из goroutine теста).
func e2eDo(t *testing.T, srv *httptest.Server, method, path string, body, out any) int {
	t.Helper()
	code, raw, err := e2eRequest(srv, method, path, body)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			t.Fatalf("decode %s %s (%d): %v: %s", method, path, code, err, raw)
		}
	}
	return code
}

func e2eWantCode(t *testing.T, what string, got, want int) {
	t.Helper()
	if got != want {
		t.Fatalf("%s: код %d, хотим %d", what, got, want)
	}
}

// hasStart — присутствует ли данный старт в списке сетки (без суждений о
// её составе и статусах — это вотчина Store-тестов, ADR-0005).
func hasStart(slots []Slot, start string) bool {
	for _, sl := range slots {
		if sl.Start == start {
			return true
		}
	}
	return false
}

// e2eTomorrow — «завтра» по дате сервера: тест и сервер — один процесс,
// time.Now общий; день в будущем — ни один интервал не «прошедший».
func e2eTomorrow() string {
	return time.Now().AddDate(0, 0, 1).Format("2006-01-02")
}

type e2eBookingView struct {
	ID          string `json:"id"`
	MeetingID   string `json:"meetingId"`
	Start       string `json:"start"`
	Email       string `json:"email"`
	MeetingName string `json:"meetingName"`
}

// TestE2E_HappyCycle — полный цикл посетителя через HTTP JSON:
// границы дня + встреча (201) → список → слоты → запись (201) →
// bookings?email видит запись → отмена (204) → запись создаётся заново (201).
// По ADR-0005 здесь проверяются коды ответов и присутствие запрошенного
// старта в списке; состав и статусы сетки — это Store-матрица, она покрыта
// тестами на Store-шовне и в e2e не дублируется.
func TestE2E_HappyCycle(t *testing.T) {
	srv := e2eServer(t)
	date := e2eTomorrow()

	// Границы рабочего дня пары (organizer, date) через контрактный эндпоинт.
	var hours hoursView
	code := e2eDo(t, srv, http.MethodPut, "/api/workhours",
		hoursView{Owner: "boss@x.io", Date: date, Hours: Hours{WorkStart: "09:00", WorkEnd: "12:00"}}, &hours)
	e2eWantCode(t, "PUT /api/workhours", code, http.StatusOK)
	if hours.WorkStart != "09:00" || hours.WorkEnd != "12:00" {
		t.Errorf("эхо границ дня: %+v", hours)
	}

	var meeting Meeting
	code = e2eDo(t, srv, http.MethodPost, "/api/meetings", map[string]any{
		"name":        "E2E-встреча",
		"description": "Сквозной HTTP-сценарий",
		"durationMin": 30,
		"ownerEmail":  "boss@x.io",
		"ownerName":   "Босс",
		"date":        date,
	}, &meeting)
	e2eWantCode(t, "POST /api/meetings", code, http.StatusCreated)
	if meeting.ID == "" {
		t.Fatalf("созданная встреча без id: %+v", meeting)
	}

	var meetings []*Meeting
	code = e2eDo(t, srv, http.MethodGet, "/api/meetings", nil, &meetings)
	e2eWantCode(t, "GET /api/meetings", code, http.StatusOK)
	found := false
	for _, m := range meetings {
		if m.ID == meeting.ID {
			found = true
		}
	}
	if !found {
		t.Fatalf("встреча %s не видна в GET /api/meetings (%d шт.)", meeting.ID, len(meetings))
	}

	const first = "09:00" // старт из сохранённых через API границ 09:00–12:00
	var slots []Slot
	code = e2eDo(t, srv, http.MethodGet, "/api/meetings/"+meeting.ID+"/slots", nil, &slots)
	e2eWantCode(t, "GET slots", code, http.StatusOK)
	if !hasStart(slots, first) {
		t.Fatalf("запрошенный старт %s отсутствует в списке сетки: %+v", first, slots)
	}

	var booking Booking
	code = e2eDo(t, srv, http.MethodPost, "/api/meetings/"+meeting.ID+"/book",
		map[string]string{"start": first, "name": "Иван", "email": "ivan@example.com"}, &booking)
	e2eWantCode(t, "POST book", code, http.StatusCreated)
	if booking.Start != first || booking.MeetingID != meeting.ID {
		t.Errorf("запись не на выбранный интервал: %+v", booking)
	}

	var views []e2eBookingView
	code = e2eDo(t, srv, http.MethodGet, "/api/bookings?email=ivan@example.com", nil, &views)
	e2eWantCode(t, "GET bookings", code, http.StatusOK)
	if len(views) != 1 || views[0].ID != booking.ID || views[0].MeetingName != "E2E-встреча" {
		t.Fatalf("bookings?email: %+v, хотим ровно запись %s встречи", views, booking.ID)
	}

	code = e2eDo(t, srv, http.MethodDelete, "/api/bookings/"+booking.ID+"?email=ivan@example.com", nil, nil)
	e2eWantCode(t, "DELETE booking", code, http.StatusNoContent)

	// После DELETE запись можно создать заново: освобождение интервала
	// выражаем кодом 201, а не пересчётом статусов сетки (ADR-0005).
	code = e2eDo(t, srv, http.MethodPost, "/api/meetings/"+meeting.ID+"/book",
		map[string]string{"start": first, "name": "Иван", "email": "ivan@example.com"}, nil)
	e2eWantCode(t, "POST book после DELETE", code, http.StatusCreated)
}

// TestE2E_BookingRace — контрактная гарантия: два одновременных POST book
// на один Интервал — ровно один 201 и один 409 (SQLite сериализует
// писателей, busy_timeout дожидается). Повторяем 5 раз на разных
// интервалах: гонка по определению нестабильна, ловим любой «оба 201».
func TestE2E_BookingRace(t *testing.T) {
	srv := e2eServer(t)

	var meeting Meeting
	code := e2eDo(t, srv, http.MethodPost, "/api/meetings", map[string]any{
		"name":        "Гонка",
		"durationMin": 30,
		"ownerEmail":  "boss@x.io",
		"date":        e2eTomorrow(),
	}, &meeting)
	e2eWantCode(t, "POST /api/meetings", code, http.StatusCreated)

	// Свободные интервалы дня берём по одному на повтор: победитель гонки
	// занимает выбранный интервал, следующий повтор стартует с чистого.
	var slots []Slot
	code = e2eDo(t, srv, http.MethodGet, "/api/meetings/"+meeting.ID+"/slots", nil, &slots)
	e2eWantCode(t, "GET slots", code, http.StatusOK)
	var free []string
	for _, sl := range slots {
		if sl.Status == SlotFree {
			free = append(free, sl.Start)
		}
	}
	const repeats = 5
	if len(free) < repeats {
		t.Fatalf("свободных интервалов %d, нужно %d", len(free), repeats)
	}

	for i := 0; i < repeats; i++ {
		start := free[i]
		var wg sync.WaitGroup
		codes := make([]int, 2)
		errs := make([]error, 2)
		for g := 0; g < 2; g++ {
			wg.Add(1)
			go func(g int) {
				defer wg.Done()
				codes[g], _, errs[g] = e2eRequest(srv, http.MethodPost,
					"/api/meetings/"+meeting.ID+"/book",
					map[string]string{"start": start,
						"name":  fmt.Sprintf("Посетитель %d", g),
						"email": fmt.Sprintf("v%d@example.com", g)})
			}(g)
		}
		wg.Wait()

		for g := 0; g < 2; g++ {
			if errs[g] != nil {
				t.Fatalf("повтор %d, транспорт goroutine %d: %v", i, g, errs[g])
			}
		}
		ok201, conflict409 := 0, 0
		for _, c := range codes {
			switch c {
			case http.StatusCreated:
				ok201++
			case http.StatusConflict:
				conflict409++
			}
		}
		if ok201 != 1 || conflict409 != 1 {
			t.Errorf("повтор %d, интервал %s: коды %v, хотим ровно один 201 и один 409", i, start, codes)
		}
	}
}

// TestE2E_PreviewStepCodes — единая конвенция кодов для шага: нечисловой
// step — 400 (синтаксис query), значение вне 1..duration — 422
// ErrStartStepRange (доменное правило, как при создании встречи).
func TestE2E_PreviewStepCodes(t *testing.T) {
	srv := e2eServer(t)
	path := "/api/preview/slots?date=" + e2eTomorrow() + "&duration=60&step="
	for _, c := range []struct {
		step string
		want int
	}{
		{"30", http.StatusOK},                  // валидный шаг
		{"abc", http.StatusBadRequest},         // не число — синтаксис query
		{"0", http.StatusUnprocessableEntity},  // вне 1..duration — домен
		{"90", http.StatusUnprocessableEntity}, // больше длительности — домен
	} {
		code, _, err := e2eRequest(srv, http.MethodGet, path+c.step, nil)
		if err != nil {
			t.Fatalf("step=%q: %v", c.step, err)
		}
		e2eWantCode(t, "GET preview slots step="+c.step, code, c.want)
	}
	// То же правило действует и для окна доступности.
	code, _, err := e2eRequest(srv, http.MethodGet, "/api/preview/availability?duration=60&step=90", nil)
	if err != nil {
		t.Fatal(err)
	}
	e2eWantCode(t, "GET preview availability step=90", code, http.StatusUnprocessableEntity)
}
