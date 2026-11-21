-- 0002: состояние Записи для Отмены Записи Посетителем (GLOSSARY: Запись).
-- Аддитивная: существующие строки получают дефолт 'active'.
-- Возможные значения: 'active' | 'cancelled' (терминальный).

ALTER TABLE bookings ADD COLUMN state TEXT NOT NULL DEFAULT 'active';
