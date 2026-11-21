// Тонкая обёртка над типобезопасным SDK (openapi-fetch + сгенерированные
// пути/схемы). Все имена ответов/запросов проверяются типами из schema.d.ts,
// который собирается из TypeSpec-контракта (mise run codegen).

import createClient from "openapi-fetch";
import type { paths } from "./api/schema";
import type {
  BookingView,
  DayAvailability,
  Meeting,
  Slot,
  WorkHours,
} from "./types";

export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message);
  }
}

const client = createClient<paths>();

// Общая обработка ошибок: тело { "error": "..." } из ApiError<Code>.
function ensureOk(res: {
  response: Response;
  error?: { error?: string };
}): void {
  if (!res.response.ok) {
    throw new ApiError(
      res.response.status,
      res.error?.error ?? res.response.statusText,
    );
  }
}

export const api = {
  // Границы рабочего дня: по почте организатора на конкретную дату (дефолт 09:00–18:00).
  workHours: async (owner: string, date: string): Promise<WorkHours> => {
    const { data, ...res } = await client.GET("/api/workhours", {
      params: { query: { owner, date } },
    });
    ensureOk(res);
    return data as WorkHours;
  },

  setWorkHours: async (
    owner: string,
    date: string,
    workStart: string,
    workEnd: string,
  ): Promise<WorkHours> => {
    const { data, ...res } = await client.PUT("/api/workhours", {
      body: { owner, date, workStart, workEnd },
    });
    ensureOk(res);
    return data as WorkHours;
  },

  meetings: async (owner?: string): Promise<Meeting[]> => {
    const { data, ...res } = await client.GET("/api/meetings", {
      params: { query: { owner: owner || undefined } },
    });
    ensureOk(res);
    return data as Meeting[];
  },

  createMeeting: async (m: {
    name: string;
    description: string;
    durationMin: number;
    startStepMin?: number;
    ownerEmail: string;
    ownerName: string;
    date: string;
    timezone?: string;
  }): Promise<Meeting> => {
    const { data, ...res } = await client.POST("/api/meetings", { body: m });
    ensureOk(res);
    return data as Meeting;
  },

  deleteMeeting: async (id: string): Promise<void> => {
    const res = await client.DELETE("/api/meetings/{id}", {
      params: { path: { id } },
    });
    ensureOk(res);
  },

  // Сетка интервалов встречи: день фиксирован (встреча назначена на дату).
  slots: async (id: string): Promise<Slot[]> => {
    const { data, ...res } = await client.GET("/api/meetings/{id}/slots", {
      params: { path: { id } },
    });
    ensureOk(res);
    return data as Slot[];
  },

  // Превью сетки до сохранения встречи (Сценарий 1); step=0/undefined — «как длительность»,
  // timezone — зона-фолбэк до первого сохранения профиля (переданная > профиль > UTC).
  previewAvailability: async (
    duration: number,
    owner: string,
    step?: number,
    timezone?: string,
  ): Promise<DayAvailability[]> => {
    const { data, ...res } = await client.GET("/api/preview/availability", {
      params: {
        query: {
          duration,
          owner: owner || undefined,
          step: step && step > 0 ? step : undefined,
          timezone: timezone || undefined,
        },
      },
    });
    ensureOk(res);
    return data as DayAvailability[];
  },

  previewSlots: async (
    owner: string,
    date: string,
    duration: number,
    step?: number,
    timezone?: string,
  ): Promise<Slot[]> => {
    const { data, ...res } = await client.GET("/api/preview/slots", {
      params: {
        query: {
          owner: owner || undefined,
          date,
          duration,
          step: step && step > 0 ? step : undefined,
          timezone: timezone || undefined,
        },
      },
    });
    ensureOk(res);
    return data as Slot[];
  },

  book: async (
    id: string,
    payload: { start: string; name: string; email: string },
  ): Promise<void> => {
    const res = await client.POST("/api/meetings/{id}/book", {
      params: { path: { id } },
      body: payload,
    });
    ensureOk(res);
  },

  bookings: async (email: string): Promise<BookingView[]> => {
    const { data, ...res } = await client.GET("/api/bookings", {
      params: { query: { email } },
    });
    ensureOk(res);
    return data as BookingView[];
  },

  // Отмена Записи Посетителем; совпадение email проверяет сервер
  // (409 — не совпал, 422 — запись уже завершилась).
  cancelBooking: async (id: string, email: string): Promise<void> => {
    const res = await client.DELETE("/api/bookings/{id}", {
      params: { path: { id }, query: { email } },
    });
    ensureOk(res);
  },
};
