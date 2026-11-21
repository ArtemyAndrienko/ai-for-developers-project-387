-- 0001: базовая схема календаря (ADR-0001: SQLite на modernc.org/sqlite).
-- Email колонируется NOCASE: занятость и списки записей сопоставляются по
-- почте без учёта регистра (доменное правило из GLOSSARY.md).

CREATE TABLE meetings (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	duration_min INTEGER NOT NULL,
	owner_email TEXT NOT NULL COLLATE NOCASE,
	owner_name TEXT NOT NULL DEFAULT '',
	date TEXT NOT NULL,
	created_at TEXT NOT NULL
);

CREATE TABLE bookings (
	id INTEGER PRIMARY KEY AUTOINCREMENT,
	meeting_id INTEGER NOT NULL REFERENCES meetings(id) ON DELETE CASCADE,
	date TEXT NOT NULL,
	start TEXT NOT NULL,
	end TEXT NOT NULL,
	name TEXT NOT NULL,
	email TEXT NOT NULL COLLATE NOCASE,
	created_at TEXT NOT NULL
);

CREATE INDEX idx_bookings_owner_date ON bookings(meeting_id, date);

CREATE TABLE work_hours (
	owner_email TEXT NOT NULL COLLATE NOCASE,
	date TEXT NOT NULL,
	work_start TEXT NOT NULL,
	work_end TEXT NOT NULL,
	PRIMARY KEY (owner_email, date)
);
