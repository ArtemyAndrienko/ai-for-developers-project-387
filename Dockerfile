# Календарь звонков — единый образ: SPA запечена в Go-бинарь (go:embed),
# приложение слушает $PORT (convention Render).

# 1) фронтенд
FROM node:24 AS web
WORKDIR /app
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

# 2) бэкенд с встроенным dist
FROM golang:1.27 AS app
WORKDIR /src
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
COPY --from=web /app/dist/ web/dist/
RUN CGO_ENABLED=0 go build -o /out/calendar-api .

# 3) рантайм
FROM alpine:3.20
RUN adduser -D -h /srv app && mkdir -p /srv/data && chown -R app /srv
WORKDIR /srv
COPY --from=app /out/calendar-api /usr/local/bin/calendar-api
USER app
ENV PORT=8000
ENV DATA_PATH=/srv/data/calendar.db
EXPOSE 8000
ENTRYPOINT ["calendar-api"]
