package main

import (
	"fmt"
	"strings"
)

// Окно доступности: сколько дней вперёд можно выбрать (Сценарий 1, шаг 2).
const windowDays = 14

// Специальные значения nowMin для GenerateSlots:
//
//	DayInPast   — весь день уже прошёл;
//	DayInFuture — день ещё не наступил (ни один интервал не «past»).
const (
	DayInPast   = 24*60 + 1
	DayInFuture = -1
)

// Статусы интервала (интервала).
type SlotStatus string

const (
	SlotFree  SlotStatus = "free"  // свободен
	SlotTaken SlotStatus = "taken" // занят (в т.ч. другой встречей того же организатора)
	SlotPast  SlotStatus = "past"  // время начала уже прошло
)

type Slot struct {
	Start  string     `json:"start"`
	End    string     `json:"end"`
	Status SlotStatus `json:"status"`
}

// Interval — занятый отрезок дня в минутах от полуночи, [Start, End).
type Interval struct {
	Start int
	End   int
}

// ParseHM строго разбирает "HH:MM" (две цифры, двоеточие, две цифры)
// в минуты от полуночи.
func ParseHM(s string) (int, bool) {
	if len(s) != 5 || s[2] != ':' || !isDigit(s[0]) || !isDigit(s[1]) || !isDigit(s[3]) || !isDigit(s[4]) {
		return 0, false
	}
	h := int(s[0]-'0')*10 + int(s[1]-'0')
	m := int(s[3]-'0')*10 + int(s[4]-'0')
	if h > 23 || m > 59 {
		return 0, false
	}
	return h*60 + m, true
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }

// FormatHM превращает минуты от полуночи в "HH:MM".
func FormatHM(min int) string {
	return fmt.Sprintf("%02d:%02d", min/60, min%60)
}

// GenerateSlots разбивает рабочий день на интервалы длительностью durationMin:
// старты идут от startHM с шагом stepMin (stepMin <= 0 — шаг равен длительности),
// предлагаются только старты, у которых полная длительность вписывается в день
// (ADR-0003: «Хвостовой интервал» упразднён, остаток дня не рисуется).
// busy — занятые отрезки: интервал помечается SlotTaken при любом пересечении,
// поэтому одна Запись блокирует все пересекающиеся старты плотной сетки,
// в т.ч. из другой встречи (с другим шагом) того же организатора.
func GenerateSlots(startHM, endHM string, durationMin, stepMin, nowMin int, busy []Interval) []Slot {
	start, ok1 := ParseHM(startHM)
	end, ok2 := ParseHM(endHM)
	if !ok1 || !ok2 || durationMin <= 0 || end <= start {
		return nil
	}
	step := stepMin
	if step <= 0 {
		step = durationMin
	}
	slots := make([]Slot, 0, (end-start)/step+1)
	for m := start; m+durationMin <= end; m += step {
		e := m + durationMin
		s := Slot{Start: FormatHM(m), End: FormatHM(e)}
		switch {
		case overlapsAny(m, e, busy):
			s.Status = SlotTaken // занятость приоритетнее «прошло»
		case m <= nowMin:
			s.Status = SlotPast // начало совпадает с "сейчас" или раньше — уже нельзя
		default:
			s.Status = SlotFree
		}
		slots = append(slots, s)
	}
	return slots
}

func overlapsAny(s, e int, busy []Interval) bool {
	for _, b := range busy {
		if s < b.End && b.Start < e {
			return true
		}
	}
	return false
}

// FreeCount считает свободные интервалы за день (для бейджа «18 св.»).
func FreeCount(slots []Slot) int {
	n := 0
	for _, s := range slots {
		if s.Status == SlotFree {
			n++
		}
	}
	return n
}

// SlotAllowed находит интервал с таким началом и говорит, можно ли его занять.
func SlotAllowed(slots []Slot, start string) (Slot, bool) {
	for _, s := range slots {
		if s.Start == start {
			return s, s.Status == SlotFree
		}
	}
	return Slot{}, false
}

// ValidEmail — минимальная проверка почты организатора/посетителя.
func ValidEmail(s string) bool {
	s = strings.TrimSpace(s)
	at := strings.Index(s, "@")
	return at > 0 && at < len(s)-1 && !strings.ContainsAny(s, " \t")
}
