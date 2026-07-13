# Hopper Architecture

## Назначение

Hopper - self-hosted файловое облако для передачи и хранения файлов между устройствами. v1 строится вокруг одного инстанса, локального дискового хранилища, SQLite и веб-интерфейса/PWA.

## Зафиксированный стек

- Backend: Go, `net/http`, `chi` router, `gorilla/websocket`.
- Database: SQLite.
- DB access: `database/sql` с SQL-миграциями в `backend/migrations`; ORM пока не вводится, чтобы не усложнять self-hosted v1.
- File storage: локальная файловая система в Docker volume, физические имена файлов только по UUID.
- Frontend: React + Vite, PWA manifest.
- Reverse proxy/TLS: Caddy с автоматическим TLS через Let's Encrypt.
- Deploy: `docker-compose.yml` + `.env.example`, без install-скриптов.

## Структура репозитория

```text
/
├── PROJECT_SPEC.md
├── ARCHITECTURE.md
├── PROGRESS.md
├── README.md
├── docker-compose.yml
├── .env.example
├── backend/
│   ├── cmd/
│   │   └── hopper/
│   │       └── main.go
│   ├── internal/
│   │   ├── auth/
│   │   ├── config/
│   │   ├── db/
│   │   ├── files/
│   │   ├── httpapi/
│   │   ├── middleware/
│   │   ├── share/
│   │   └── ws/
│   ├── migrations/
│   ├── go.mod
│   └── Dockerfile
├── frontend/
│   ├── public/
│   ├── src/
│   │   ├── api/
│   │   ├── components/
│   │   ├── pages/
│   │   ├── state/
│   │   └── main.tsx
│   ├── package.json
│   └── Dockerfile
└── caddy/
    └── Caddyfile
```

## Конфигурация

Переменные окружения задаются через `.env` на основе `.env.example`.

- `APP_DOMAIN`: домен для Caddy.
- `APP_ENV`: `development` или `production`.
- `HTTP_ADDR`: адрес backend внутри контейнера/процесса, по умолчанию `:8080`.
- Backend не публикуется на host network и доступен только frontend-контейнеру внутри Compose-сети.
- `DATABASE_PATH`: путь к SQLite-файлу внутри volume.
- `STORAGE_PATH`: путь к каталогу с физическими файлами.
- `JWT_ACCESS_SECRET`: секрет access JWT.
- Refresh tokens являются случайными opaque-токенами и хранятся в БД только как SHA-256; отдельный JWT secret для них не используется.
- `ACCESS_TOKEN_TTL`: время жизни access token, например `15m`.
- `REFRESH_TOKEN_TTL`: время жизни refresh token, например `720h`.
- `REGISTRATION_ENABLED`: разрешена ли регистрация новых пользователей.
- `FIRST_RUN_ADMIN_LOGIN`: логин первого аккаунта, который backend создает при пустой БД (`admin` по умолчанию).
- `FIRST_RUN_ADMIN_PASSWORD`: optional пароль первого аккаунта; если пустой, backend генерирует случайный пароль и печатает его в лог один раз при создании пользователя.
- `MAX_UPLOAD_BYTES`: лимит размера одного файла.
- `INSTANCE_QUOTA_BYTES`: общий лимит хранилища.
- `AUTH_RATE_LIMIT_PER_MINUTE`: лимит запросов auth endpoint на IP.

## Модель данных

SQLite схема v1:

```sql
CREATE TABLE users (
  id TEXT PRIMARY KEY,
  login TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE refresh_tokens (
  id TEXT PRIMARY KEY,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  token_hash TEXT NOT NULL UNIQUE,
  expires_at TEXT NOT NULL,
  revoked_at TEXT,
  created_at TEXT NOT NULL
);

CREATE TABLE folders (
  id TEXT PRIMARY KEY,
  owner_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  parent_id TEXT REFERENCES folders(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE files (
  id TEXT PRIMARY KEY,
  owner_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  folder_id TEXT REFERENCES folders(id) ON DELETE SET NULL,
  original_name TEXT NOT NULL,
  storage_name TEXT NOT NULL UNIQUE,
  mime_type TEXT NOT NULL,
  size_bytes INTEGER NOT NULL,
  checksum_sha256 TEXT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE shares (
  id TEXT PRIMARY KEY,
  file_id TEXT NOT NULL REFERENCES files(id) ON DELETE CASCADE,
  owner_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  token_hash TEXT NOT NULL UNIQUE,
  expires_at TEXT NOT NULL,
  max_downloads INTEGER,
  download_count INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  revoked_at TEXT
);

CREATE UNIQUE INDEX idx_folders_owner_root_name ON folders(owner_id, name) WHERE parent_id IS NULL;
CREATE UNIQUE INDEX idx_folders_owner_parent_name ON folders(owner_id, parent_id, name) WHERE parent_id IS NOT NULL;
```

Правила хранения:

- `files.original_name` используется только для отображения и `Content-Disposition`.
- На диске файл лежит как `<STORAGE_PATH>/<storage_name>`, где `storage_name` - UUID без пользовательского ввода.
- Для любых операций с папками запрещены `..`, абсолютные пути, пустые сегменты и системные разделители в имени.

## Авторизация

1. Пользователь отправляет `POST /api/auth/login` с логином и паролем.
2. Backend проверяет rate limit по IP, находит пользователя и сверяет пароль через bcrypt или argon2.
3. Backend возвращает access JWT в JSON, а случайный refresh token устанавливает как `HttpOnly; SameSite=Strict` cookie с путем `/api/auth`.
4. Access JWT хранится только в памяти frontend и передается в `Authorization: Bearer <token>`; browser storage для токенов не используется.
5. Refresh token хранится в БД только в виде SHA-256, ротируется через `POST /api/auth/refresh` и получает флаг `Secure` при HTTPS.
6. `POST /api/auth/logout` отзывает refresh token и удаляет cookie. Версия авторизации пользователя немедленно отзывает старые access tokens после смены credentials.
7. WebSocket `/api/ws` принимает access token через query-параметр `token` или Authorization header, затем использует ту же JWT-проверку.

## API-контракты

Все ответы API возвращают JSON, кроме скачивания файлов.

### Auth

`POST /api/auth/register`

Запрос:

```json
{
  "login": "user",
  "password": "plain-password"
}
```

Ответ `201`:

```json
{
  "user": {
    "id": "uuid",
    "login": "user"
  }
}
```

Ошибки: `403` если регистрация отключена, `409` если логин занят, `429` при rate limit.

`POST /api/auth/login`

Запрос:

```json
{
  "login": "user",
  "password": "plain-password"
}
```

Ответ `200`:

```json
{
  "accessToken": "jwt",
  "refreshToken": "opaque-token",
  "expiresIn": 900,
  "user": {
    "id": "uuid",
    "login": "user"
  }
}
```

`POST /api/auth/refresh`

Запрос:

```json
{
  "refreshToken": "opaque-token"
}
```

Ответ `200`:

```json
{
  "accessToken": "jwt",
  "refreshToken": "new-opaque-token",
  "expiresIn": 900
}
```

`GET /api/auth/me`

Заголовок: `Authorization: Bearer <accessToken>`

Ответ `200`:

```json
{
  "user": {
    "id": "uuid",
    "login": "user"
  }
}
```

`PATCH /api/auth/me`

Требует access JWT и проверку текущего пароля. Запрос:

```json
{
  "currentPassword": "current-password",
  "newLogin": "new-login",
  "newPassword": "optional-new-password"
}
```

Хотя бы одно из полей `newLogin`/`newPassword` должно быть передано. При успехе backend атомарно обновляет учетные данные, отзывает все ранее выданные refresh tokens и возвращает новую пару access/refresh tokens с обновленным пользователем.

### Files and folders

`GET /api/files?folderId=<uuid|null>&page=1&pageSize=50`

Ответ `200`:

```json
{
  "items": [
    {
      "id": "uuid",
      "type": "file",
      "name": "photo.jpg",
      "mimeType": "image/jpeg",
      "sizeBytes": 12345,
      "createdAt": "2026-07-01T12:00:00Z",
      "updatedAt": "2026-07-01T12:00:00Z"
    },
    {
      "id": "uuid",
      "type": "folder",
      "name": "Documents",
      "createdAt": "2026-07-01T12:00:00Z",
      "updatedAt": "2026-07-01T12:00:00Z"
    }
  ],
  "page": 1,
  "pageSize": 50,
  "total": 2
}
```

`POST /api/files/upload`

Запрос: `multipart/form-data`

- `file`: binary file.
- `folderId`: optional folder UUID.

Ответ `201`:

```json
{
  "id": "uuid",
  "name": "photo.jpg",
  "mimeType": "image/jpeg",
  "sizeBytes": 12345
}
```

Ошибки: `413` при превышении `MAX_UPLOAD_BYTES`, `409` при превышении `INSTANCE_QUOTA_BYTES`.

`GET /api/files/{id}`

Ответ `200`: бинарный поток файла с заголовками `Content-Type`, `Content-Length`, `Content-Disposition`.

`DELETE /api/files/{id}`

Ответ `204`.

`PATCH /api/files/{id}`

Запрос:

```json
{
  "name": "new-name.jpg",
  "folderId": "uuid-or-null"
}
```

Ответ `200`:

```json
{
  "id": "uuid",
  "name": "new-name.jpg",
  "folderId": "uuid-or-null",
  "updatedAt": "2026-07-01T12:00:00Z"
}
```

`POST /api/folders`

Запрос:

```json
{
  "name": "Documents",
  "parentId": "uuid-or-null"
}
```

Ответ `201`:

```json
{
  "id": "uuid",
  "name": "Documents",
  "parentId": "uuid-or-null"
}
```

### Sharing

`POST /api/files/{id}/share`

Запрос:

```json
{
  "expiresInSeconds": 86400,
  "maxDownloads": 5
}
```

Ответ `201`:

```json
{
  "url": "https://example.com/s/public-token",
  "expiresAt": "2026-07-02T12:00:00Z",
  "maxDownloads": 5
}
```

`GET /s/{token}`

Ответ `200`: бинарный поток файла. Проверяет срок действия, отзыв и лимит скачиваний.

### Messages

- `GET /api/messages/` — последние 100 заметок текущего пользователя.
- `POST /api/messages/` с `{ "body": "text" }` — создать текстовую заметку до 4000 символов.
- `DELETE /api/messages/{id}` — удалить принадлежащую пользователю заметку.

Сообщения рендерятся frontend только как текст, без HTML-интерпретации.

### Preview

`GET /api/files/{id}/preview` определяет фактический MIME по содержимому. Inline разрешены только JPEG/PNG/GIF/WebP, PDF, базовые audio/video и plain text до 1 MiB. HTML, SVG и неизвестные бинарники возвращают `415`; ответы получают `nosniff`, sandbox CSP и `no-store`.

### WebSocket

`WS /api/ws`

Авторизация: access token через query-параметр `token` или `Authorization: Bearer <token>`. События отправляются только соединениям того пользователя, которому принадлежит измененный ресурс.

События от сервера:

```json
{
  "type": "file.created",
  "payload": {
    "id": "uuid",
    "name": "photo.jpg"
  }
}
```

```json
{
  "type": "upload.progress",
  "payload": {
    "uploadId": "uuid",
    "receivedBytes": 1024,
    "totalBytes": 12345
  }
}
```

Типы событий v1: `file.created`, `file.updated`, `file.deleted`, `folder.created`, `folder.updated`, `folder.deleted`, `upload.progress`.

## Формат ошибок

```json
{
  "error": {
    "code": "validation_error",
    "message": "Human readable message",
    "details": {}
  }
}
```

Базовые коды: `validation_error`, `unauthorized`, `forbidden`, `not_found`, `conflict`, `rate_limited`, `payload_too_large`, `quota_exceeded`, `internal_error`.

## Безопасность

- Пользовательский ввод никогда не используется как физический путь к файлу.
- Имена файлов и папок валидируются отдельно от путей.
- Пароли хэшируются bcrypt или argon2id.
- Refresh tokens и public share tokens хранятся только в виде хэша.
- Auth endpoints защищены rate limiting.
- Все endpoints, кроме `/api/auth/*` и `/s/{token}`, требуют access JWT.
- Размер upload ограничивается до чтения всего тела запроса.
- Удаление файла сначала проверяет владельца, затем удаляет запись БД и физический файл.
- Public share token генерируется криптографически стойким генератором случайных чисел.

## Docker Compose

Локальный запуск без домена:

```bash
cp .env.example .env
# заполнить JWT_ACCESS_SECRET и при необходимости квоты
docker compose up -d --build
```

Панель доступна на `http://localhost:3000`. По умолчанию frontend привязан к `127.0.0.1`; для доступа из локальной сети владелец явно задает `FRONTEND_BIND=0.0.0.0` и защищает порт firewall.

Production-запуск с доменом:

```bash
docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d --build
```

`APP_DOMAIN` указывает на домен с DNS A/AAAA записью сервера. Caddy публикует `80/443`, автоматически получает TLS-сертификат и проксирует панель; backend остается доступен только внутри Compose-сети.

Планируемые сервисы:

- `backend`: Go API, монтирует volumes `hopper-data` и `hopper-db`.
- `frontend`: сборка React/Vite или статические файлы, отдаваемые backend/Caddy.
- До подключения Caddy frontend-контейнер отдаёт React через nginx на `FRONTEND_PORT` (`3000` по умолчанию) и проксирует `/api/*` во внутренний сервис `backend:8080`. Это обеспечивает один веб-адрес для входа с компьютера, телефона и планшета.
- `caddy`: reverse proxy, публичные порты `80` и `443`, авто-TLS.

Volumes:

- `hopper-db`: SQLite database.
- `hopper-data`: загруженные файлы.
- `caddy-data`: сертификаты.
- `caddy-config`: служебная конфигурация Caddy.

## Frontend

Основные экраны v1:

- Login/Register.
- Файловый менеджер со списком файлов и папок.
- Upload через кнопку и drag and drop.
- Download/delete/rename/move actions.
- Диалог создания временной ссылки.
- PWA manifest и базовая мобильная адаптация.

Клиент хранит access token только в памяти. Refresh token хранится в `HttpOnly; SameSite=Strict` cookie, недоступной JavaScript; при перезагрузке frontend восстанавливает access token через `POST /api/auth/refresh`.
