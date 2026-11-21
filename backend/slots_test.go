package main

import (
	"testing"
)

// Полный рабочий день 09:00–18:00 с длительностью 30 и шагом 30 даёт ровно
// 18 интервалов без хвостового остатка (референс: бейдж «18 св.»).
func TestGenerateSlots_FullDayNoTail(t *testing.T) {
	slots := GenerateSlots("09:00", "18:00", 30, 30, DayInFuture, nil)
	if len(slots) != 18 {
		t.Fatalf("ожидалось 18 интервалов, получено %d", len(slots))
	}
	if slots[0].Start != "09:00" || slots[0].End != "09:30" {
		t.Errorf("первый интервал %v", slots[0])
	}
	if last := slots[17]; last.Start != "17:30" || last.End != "18:00" {
		t.Errorf("последний интервал %v", last)
	}
	for _, s := range slots {
		if s.Status != SlotFree {
			t.Errorf("интервал %v должен быть свободен", s)
		}
	}
}

// Скользящая сетка (ADR-0003): длительность 60, шаг 30 — старты каждые
// 30 минут, пока полная длительность влезает в день; последний старт
// 17:00–18:00, старт 17:30 не предлагается (длительность не влезает).
func TestGenerateSlots_SlidingGrid(t *testing.T) {
	slots := GenerateSlots("09:00", "18:00", 60, 30, DayInFuture, nil)
	if len(slots) != 17 {
		t.Fatalf("ожидалось 17 стартов (09:00…17:00), получено %d", len(slots))
	}
	if slots[0].Start != "09:00" || slots[0].End != "10:00" {
		t.Errorf("первый интервал %v", slots[0])
	}
	if last := slots[len(slots)-1]; last.Start != "17:00" || last.End != "18:00" {
		t.Errorf("последний интервал %v", last)
	}
	want := []string{"09:00", "09:30", "10:00", "10:30"}
	for i, start := range want {
		if slots[i].Start != start {
			t.Errorf("старт #%d: ожидался %s, получено %s", i, start, slots[i].Start)
		}
	}
	for _, s := range slots {
		if s.Status != SlotFree {
			t.Errorf("интервал %v должен быть свободен", s)
		}
	}
}

// «Хвостовой интервал» упразднён: остаток дня короче длительности не
// рождается никогда. 09:00–17:50 с длительностью 45 и шагом 45 — 11 стартов
// (09:00…16:30, последний целый интервал 16:30–17:15), остаток 17:15–17:50
// в сетку не попадает.
func TestGenerateSlots_NoTailNever(t *testing.T) {
	slots := GenerateSlots("09:00", "17:50", 45, 45, DayInFuture, nil)
	if len(slots) != 11 {
		t.Fatalf("ожидалось 11 интервалов, получено %d", len(slots))
	}
	last := slots[len(slots)-1]
	if last.Start != "16:30" || last.End != "17:15" {
		t.Errorf("последний интервал %v — целый, хвоста быть не должно", last)
	}
	if FreeCount(slots) != len(slots) {
		t.Errorf("все целые интервалы свободны, получили %d из %d", FreeCount(slots), len(slots))
	}
	// Касательный день короче длительности — сетка пуста, хвоста нет.
	if short := GenerateSlots("09:00", "09:40", 45, 45, DayInFuture, nil); len(short) != 0 {
		t.Errorf("день короче длительности: ожидалась пустая сетка, получено %v", short)
	}
}

func TestGenerateSlots_Statuses(t *testing.T) {
	busy := []Interval{{Start: 9 * 60, End: 9*60 + 30}, {Start: 9*60 + 30, End: 10 * 60}}
	// now = 10:00: интервалы с началом <= 10:00 — прошедшие (если не заняты).
	slots := GenerateSlots("09:00", "12:00", 30, 30, 10*60, busy)
	want := map[string]SlotStatus{
		"09:00": SlotTaken, // занят, приоритет над прошедшим
		"09:30": SlotTaken,
		"10:00": SlotPast,
		"10:30": SlotFree,
		"11:00": SlotFree,
		"11:30": SlotFree,
	}
	if len(slots) != len(want) {
		t.Fatalf("ожидалось %d интервалов, получено %d", len(want), len(slots))
	}
	for _, s := range slots {
		if want[s.Start] != s.Status {
			t.Errorf("интервал %s: статус %s, ожидался %s", s.Start, s.Status, want[s.Start])
		}
	}
}

// Плотная сетка: одна Запись 10:00–11:00 блокирует оба пересекающихся
// получасовых старта (60/30), непересекающиеся свободны.
func TestGenerateSlots_OverlapFromOtherMeeting(t *testing.T) {
	busy := []Interval{{Start: 10 * 60, End: 11 * 60}}
	slots := GenerateSlots("09:00", "12:00", 60, 60, DayInFuture, busy)
	want := map[string]SlotStatus{
		"09:00": SlotFree, // 09:00–10:00 — пересечения нет
		"10:00": SlotTaken,
		"11:00": SlotFree,
	}
	if len(slots) != 3 {
		t.Fatalf("3 шестидесятиминутных интервала, получено %d", len(slots))
	}
	for _, s := range slots {
		if want[s.Start] != s.Status {
			t.Errorf("интервал %s: статус %s, ожидался %s", s.Start, s.Status, want[s.Start])
		}
	}
}

func TestGenerateSlots_DayInPast(t *testing.T) {
	slots := GenerateSlots("09:00", "18:00", 30, 30, DayInPast, nil)
	for _, s := range slots {
		if s.Status != SlotPast {
			t.Fatalf("вчерашний день целиком должен быть past: %v", s)
		}
	}
}

func TestSlotAllowed(t *testing.T) {
	slots := GenerateSlots("09:00", "18:00", 30, 30, DayInFuture, nil)
	if _, ok := SlotAllowed(slots, "09:00"); !ok {
		t.Errorf("свободный интервал должен разрешать бронь")
	}
	if _, ok := SlotAllowed(slots, "09:15"); ok {
		t.Errorf("интервала вне сетки не существует")
	}
	// 09:00–17:50 с длительностью 45: старт 17:15 не влезает целиком —
	// интервала нет, забронировать нельзя (хвост упразднён, ADR-0003).
	tailDay := GenerateSlots("09:00", "17:50", 45, 45, DayInFuture, nil)
	if _, ok := SlotAllowed(tailDay, "17:15"); ok {
		t.Errorf("не влезающий старт не должен существовать в сетке")
	}
}

func TestParseHM(t *testing.T) {
	for _, s := range []string{"00:00", "09:05", "18:00", "23:59"} {
		m, ok := ParseHM(s)
		if !ok {
			t.Fatalf("ParseHM(%q) не разобрался", s)
		}
		if got := FormatHM(m); got != s {
			t.Errorf("FormatHM(ParseHM(%q)) = %q", s, got)
		}
	}
	for _, bad := range []string{"9:0", "24:00", "09:60", "", "abc", "090-00"} {
		if _, ok := ParseHM(bad); ok {
			t.Errorf("ParseHM(%q) должен отказать", bad)
		}
	}
}

func TestValidEmail(t *testing.T) {
	for _, okCase := range []string{"a@b.io", "ivan+test@example.com"} {
		if !ValidEmail(okCase) {
			t.Errorf("%q должна проходить", okCase)
		}
	}
	for _, bad := range []string{"", "a", "@b.io", "a@", "a b@x.io"} {
		if ValidEmail(bad) {
			t.Errorf("%q должна отклоняться", bad)
		}
	}
}
