# img-processor

Сервис асинхронной обработки изображений. Принимает файлы через REST API, публикует задачи в Apache Kafka, обрабатывает в фоне (resize, thumbnail, watermark) и сохраняет результаты в локальное хранилище или MinIO. Предоставляет веб-интерфейс для управления без curl.

---

## Содержание

- [Возможности](#возможности)
- [Архитектура](#архитектура)
- [Быстрый старт](#быстрый-старт)
- [Конфигурация](#конфигурация)
- [API](#api)
- [Веб-интерфейс](#веб-интерфейс)
- [Разработка](#разработка)
- [Структура проекта](#структура-проекта)

---

## Возможности

- **REST API** — загрузка, получение, листинг и удаление изображений
- **Асинхронная обработка** — задачи публикуются в Kafka, воркер обрабатывает в фоне
- **Три операции обработки** — ресайз до заданной ширины, генерация миниатюры, наложение водяного знака
- **Автоподбор водяного знака** — масштабируется до 20% ширины изображения, размещается в правом нижнем углу
- **Полноценные анимированные GIF** — все кадры сохраняются при resize, thumbnail и watermark
- **Три версии изображения** — `original`, `processed`, `thumb`
- **Два типа хранилища** — локальная файловая система и MinIO (S3-совместимое)
- **Polling статуса** — `GET /image/{id}/status` возвращает прогресс обработки
- **Swagger UI** — `/swagger/index.html`
- **Веб-интерфейс** — `/` для загрузки, просмотра, скачивания и удаления

---

## Архитектура

```
HTTP-запрос / Веб-интерфейс
        │
        ▼
  ImageHandler (Gin)
        │
        ▼
  ProcessorService
        │
        ├── ImageRepository  (Local / MinIO)  — хранение файлов
        └── TaskPublisher    (Kafka)           — публикация задач
                │
                ▼
         KafkaAdapter (Consumer)
                │
                ▼
         ProcessorService.ProcessTask
                │
                ├── Resize       — уменьшение до maxWidth
                ├── AddWatermark — наложение watermark.png
                └── CreateThumbnail — квадратная миниатюра
```

**Жизненный цикл задачи:**

```
pending → processing → done
                    ↘ error → DLQ (image-processing-dlq)
```

**Версии изображения:**

| Версия      | Описание                              |
|-------------|---------------------------------------|
| `original`  | Исходный файл без изменений           |
| `processed` | Ресайз + водяной знак                 |
| `thumb`     | Квадратная миниатюра (crop по центру) |

---

## Быстрый старт

### Требования

- [Docker](https://docs.docker.com/get-docker/) и Docker Compose
- Go 1.24+ (только для локальной разработки)

### Запуск через Docker Compose (рекомендуется)

```bash
# 1. Клонировать репозиторий
git clone https://github.com/RidusM/img-processor
cd img-processor

# 2. Скопировать конфиг
cp .env.example .env

# 3. Запустить всё (Kafka + MinIO + приложение)
make compose-up
```

Сервис будет доступен на `http://localhost:8080`.

### Запуск только инфраструктуры (для разработки)

```bash
# Поднять Kafka + MinIO
make infra-up

# Запустить приложение
make run
```

---

## Конфигурация

Все параметры задаются через переменные окружения (файл `.env`).

### Приложение

| Переменная    | По умолчанию      | Описание                                     |
|---------------|-------------------|----------------------------------------------|
| `APP_NAME`    | `img-processor`   | Название сервиса                             |
| `APP_VERSION` | `1.0.0`           | Версия                                       |
| `ENV`         | `local`           | Окружение: `local`, `dev`, `staging`, `prod` |

### Сервис (обработка)

| Переменная                    | По умолчанию | Описание                              |
|-------------------------------|--------------|---------------------------------------|
| `SERVICE_MAX_FILE_SIZE_MB`    | `32`         | Максимальный размер файла (МБ)        |
| `SERVICE_MAX_WIDTH`           | `1920`       | Максимальная ширина после ресайза     |
| `SERVICE_THUMB_SIZE`          | `300`        | Размер миниатюры (px, квадрат)        |
| `SERVICE_JPEG_QUALITY`        | `90`         | Качество JPEG (1–100)                 |
| `SERVICE_ENABLE_WATERMARK`    | `true`       | Включить водяной знак                 |
| `SERVICE_WATERMARK_PATH`      | `/app/assets/watermark.png` | Путь к файлу watermark |
| `SERVICE_CLEANUP_INTERVAL`    | `30m`        | Интервал очистки старых файлов        |
| `SERVICE_CLEANUP_MAX_AGE`     | `168h`       | Максимальный возраст файлов           |

### Хранилище

| Переменная                    | По умолчанию      | Описание                        |
|-------------------------------|-------------------|---------------------------------|
| `STORAGE_TYPE`                | `local`           | Тип: `local` или `minio`        |
| `STORAGE_PATH`                | `/app/storage`    | Путь для локального хранилища   |
| `STORAGE_MINIO_ENDPOINT`      | `minio:9000`      | Адрес MinIO                     |
| `STORAGE_MINIO_ACCESS_KEY`    | `minioadmin`      | Access Key                      |
| `STORAGE_MINIO_SECRET_KEY`    | `minioadmin`      | Secret Key                      |
| `STORAGE_MINIO_BUCKET`        | `img-processor`   | Название бакета                 |
| `STORAGE_MINIO_USE_SSL`       | `false`           | Использовать SSL                |
| `STORAGE_MINIO_REGION`        | `us-east-1`       | Регион                          |

### Kafka

| Переменная               | По умолчанию              | Описание                          |
|--------------------------|---------------------------|-----------------------------------|
| `KAFKA_BROKERS`          | `kafka:29092`             | Адреса брокеров (через запятую)   |
| `KAFKA_TOPIC`            | `image-processing`        | Топик для задач                   |
| `KAFKA_DLQ_TOPIC`        | `image-processing-dlq`    | Топик для неудачных задач         |
| `KAFKA_GROUP_ID`         | `img-processor-workers`   | Consumer group ID                 |
| `KAFKA_MAX_ATTEMPTS`     | `3`                       | Максимальное число попыток        |
| `KAFKA_BASE_RETRY_DELAY` | `500ms`                   | Базовая задержка перед повтором   |
| `KAFKA_MAX_RETRY_DELAY`  | `5s`                      | Максимальная задержка             |

### HTTP-сервер

| Переменная                    | По умолчанию |
|-------------------------------|--------------|
| `HTTP_HOST`                   | `0.0.0.0`    |
| `HTTP_PORT`                   | `8080`       |
| `HTTP_READ_TIMEOUT`           | `10s`        |
| `HTTP_WRITE_TIMEOUT`          | `30s`        |
| `HTTP_IDLE_TIMEOUT`           | `60s`        |
| `HTTP_SHUTDOWN_TIMEOUT`       | `10s`        |
| `HTTP_READ_HEADER_TIMEOUT`    | `5s`         |
| `HTTP_MAX_HEADER_BYTES`       | `1048576`    |

### Logger

| Переменная           | По умолчанию                     |
|----------------------|----------------------------------|
| `LOGGER_LEVEL`       | `info`                           |
| `LOGGER_FILENAME`    | `/app/logs/img-processor.log`    |
| `LOGGER_MAX_SIZE`    | `100`                            |
| `LOGGER_MAX_BACKUPS` | `3`                              |
| `LOGGER_MAX_AGE`     | `28`                             |
| `LOGGER_COMPRESS`    | `true`                           |

---

## API

Полная документация доступна в Swagger UI: `http://localhost:8080/swagger/index.html`

### `POST /upload` — Загрузить изображение

Принимает файл и опции обработки. Возвращает ID задачи, обработка происходит асинхронно.

```bash
curl -X POST http://localhost:8080/upload \
  -F "file=@photo.jpg" \
  -F 'options={"resize_width":1280,"thumbnail_size":300,"add_watermark":true}'
```

**Поддерживаемые форматы:** `jpg`, `jpeg`, `png`, `gif`, `webp`

**Опции обработки (JSON в поле `options`):**

| Поле               | Тип    | Описание                              |
|--------------------|--------|---------------------------------------|
| `resize_width`     | int    | Максимальная ширина после ресайза     |
| `resize_height`    | int    | Максимальная высота после ресайза     |
| `thumbnail_size`   | int    | Размер миниатюры (px)                 |
| `add_watermark`    | bool   | Наложить водяной знак                 |
| `quality`          | int    | Качество JPEG (1–100)                 |
| `preserve_metadata`| bool   | Сохранить метаданные                  |

**Ответ `202 Accepted`:**
```json
{
  "id": "019e13c1-d6d8-7cd6-98d8-5623781c821e",
  "status": "pending",
  "filename": "photo.jpg",
  "size": 123662,
  "message": "Image uploaded successfully"
}
```

---

### `GET /image/{id}` — Получить изображение

Возвращает файл изображения. Пока обработка не завершена — возвращает `202` со статусом.

```bash
# Получить обработанное изображение
curl http://localhost:8080/image/019e13c1-d6d8-7cd6-98d8-5623781c821e

# Получить конкретную версию
curl "http://localhost:8080/image/019e13c1-d6d8-7cd6-98d8-5623781c821e?version=original"
curl "http://localhost:8080/image/019e13c1-d6d8-7cd6-98d8-5623781c821e?version=thumb"
```

**Параметры:**

| Параметр  | Значения                          | По умолчанию |
|-----------|-----------------------------------|--------------|
| `version` | `processed`, `original`, `thumb`  | `processed`  |

---

### `GET /image/{id}/status` — Статус обработки

```bash
curl http://localhost:8080/image/019e13c1-d6d8-7cd6-98d8-5623781c821e/status
```

**Ответ `200 OK`:**
```json
{
  "id": "019e13c1-d6d8-7cd6-98d8-5623781c821e",
  "status": "done",
  "progress": 100,
  "created_at": "2026-05-11T19:42:34.276Z",
  "updated_at": "2026-05-11T19:42:36.057Z"
}
```

**Статусы:**

| Статус       | Описание                          |
|--------------|-----------------------------------|
| `pending`    | Задача создана, ожидает воркера   |
| `processing` | Воркер обрабатывает изображение   |
| `done`       | Обработка завершена успешно       |
| `error`      | Ошибка, задача отправлена в DLQ   |

---

### `GET /images` — Список изображений

```bash
curl http://localhost:8080/images
```

**Ответ `200 OK`:**
```json
[
  {
    "ID": "019e13c1-d6d8-7cd6-98d8-5623781c821e",
    "Ext": "jpg",
    "Status": "done",
    "Progress": 100
  }
]
```

---

### `DELETE /image/{id}` — Удалить изображение

Удаляет все три версии файла (original, processed, thumbnail).

```bash
curl -X DELETE http://localhost:8080/image/019e13c1-d6d8-7cd6-98d8-5623781c821e
```

**Ответ `204 No Content`**

---

### `GET /health` — Проверка работоспособности

```bash
curl http://localhost:8080/health
# {"status":"ok","time":"2026-05-11T19:42:34.276Z"}
```

---

## Веб-интерфейс

Доступен на `http://localhost:8080/`. Позволяет без curl:

- загрузить изображение через форму или drag-and-drop
- задать параметры обработки (ширина, размер миниатюры, водяной знак)
- отслеживать статус обработки в реальном времени
- переключаться между версиями (`processed` / `original` / `thumb`)
- открыть изображение в лайтбоксе
- скачать или удалить изображение

---

## Разработка

```bash
# Установить инструменты разработки
make deps

# Запустить приложение локально (требует запущенной инфраструктуры)
make run

# Собрать бинарник linux/amd64
make build

# Собрать бинарник для текущей ОС
make build-local

# Собрать Docker-образ
make build-docker

# Запустить тесты с race detector и покрытием
make test

# Форматирование кода
make format

# Линтер
make lint

# Сгенерировать Swagger-документацию
make swagger

# Pre-commit (format + lint + swagger)
make pre-commit

# Очистить артефакты сборки
make clean
```

---

## Структура проекта

```
img-processor/
├── assets/
│   └── watermark.png            # Файл водяного знака
├── cmd/
│   └── img-processor/
│       └── main.go              # Точка входа
├── configs/                     # Конфиги для окружений
│   ├── dev.env
│   └── test.env
├── docs/                        # Swagger (генерируется make swagger)
├── internal/
│   ├── app/
│   │   └── app.go               # Инициализация и запуск компонентов
│   ├── config/
│   │   └── config.go            # Конфигурация через env-переменные
│   ├── entity/                  # Доменные типы: Image, Task, Status
│   ├── processor/
│   │   ├── image.go             # Resize, Thumbnail, Watermark, GIF-поддержка
│   │   └── options.go           # Опции процессора
│   ├── repository/
│   │   └── image.go             # Работа с хранилищем (Local / MinIO)
│   ├── service/
│   │   ├── service.go           # Бизнес-логика: Upload, ProcessTask, ListImages
│   │   └── options.go           # Опции сервиса
│   └── transport/
│       ├── http/                # HTTP handlers, middleware, роутер (Gin)
│       └── kafka/
│           └── kafka.go         # KafkaAdapter: producer + consumer
├── pkg/
│   └── storage/
│       ├── local.go             # Локальное файловое хранилище
│       ├── minio.go             # MinIO хранилище
│       └── storage.go           # Интерфейс Provider
├── web/
│   └── index.html               # Веб-интерфейс
├── .env.example
├── docker-compose.yml
├── Dockerfile
├── Makefile
└── go.mod
```