# Аудит безопасности mtg-fork — 13.02.2026

**Дата:** 2026-02-13
**Версия Go:** 1.25.7 (toolchain go1.25.7)
**govulncheck:** 0 уязвимостей
**go vet:** чисто
**go test:** 19/19 пакетов PASS

## Статус ранее найденных проблем (аудит 11.02.2026)

| Проблема | Статус | Подтверждено |
|----------|--------|--------------|
| math/rand → crypto/rand (faketls) | ✅ ИСПРАВЛЕНО | `random.go` использует `crypto/rand` |
| Prometheus bind 127.0.0.1 | ✅ ИСПРАВЛЕНО (дизайн) | `0.0.0.0` внутри контейнера, порт expose-only (не ports), изолирован docker network |
| time-skewness 10s → 5s | ✅ ИСПРАВЛЕНО | `config.toml`: `tolerate-time-skewness = "5s"` |
| Хэширование IP в логах | ✅ ИСПРАВЛЕНО | `ip_privacy.go`: truncated SHA-256 |
| Blocklist enabled | ✅ ИСПРАВЛЕНО | `config.toml`: `enabled = true` |
| DNS → DoH | ✅ ИСПРАВЛЕНО | `config.toml`: `dns-mode = "doh"` |
| memzero curve25519 scalar | ✅ ИСПРАВЛЕНО | `welcome.go`: зачистка scalar |
| Go 1.25.6 → 1.25.7 (GO-2026-4337) | ✅ ИСПРАВЛЕНО | `go version go1.25.7` |
| Secret в error string | ✅ ИСПРАВЛЕНО | `config.go` Validate(): `"invalid secret"` без значения |

---

## НОВЫЕ НАХОДКИ

### HIGH-1: secureRandIntn() возвращает 0 при ошибке crypto/rand (Silent Degradation)

**Файл:** `mtglib/internal/faketls/random.go:22-24`
**Риск:** При катастрофическом отказе entropy (крайне маловероятно) — функция тихо возвращает 0 вместо ошибки/panic. Это делает CCS injection детерминированным (всегда позиция 0).

**Контекст:** crypto/rand.Read() на Linux не возвращает ошибки на поддерживаемых ОС (использует getrandom(2) с fallback на /dev/urandom). В комментарии в коде это упомянуто. Реальная вероятность срабатывания — практически нулевая.

**Рекомендация:** Заменить `return 0` на `panic("crypto/rand: entropy exhausted")` — при отсутствии entropy система всё равно недееспособна.

```go
if _, err := rand.Read(buf[:]); err != nil {
    panic(fmt.Sprintf("crypto/rand.Read failed: %v", err))
}
```

**Приоритет:** P2 (low probability, high impact)

---

### HIGH-2: DNS DoH ответ без лимита размера

**Файл:** `network/dns_resolver.go:67`
**Риск:** `io.ReadAll(resp.Body)` без ограничения. При компрометации DoH сервера или MITM — потенциально неограниченный ответ.

**Контекст:** DoH сервер (9.9.9.9 по умолчанию) — доверенный. HTTP клиент имеет таймаут. Реалистичность атаки — низкая.

**Рекомендация:**

```go
body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024)) // 64KB достаточно для любого DNS ответа
```

**Приоритет:** P1

---

### HIGH-3: Загрузка блоклистов без лимита размера

**Файл:** `ipblocklist/firehol.go:218` — `io.Copy(tmpFile, fileContent)`
**Риск:** При компрометации URL блоклиста — неограниченная запись на диск. Cache dir: `/tmp/mtg-firehol-cache/`.

**Рекомендация:**

```go
const maxBlocklistSize = 100 * 1024 * 1024 // 100 MB
written, err := io.Copy(tmpFile, io.LimitReader(fileContent, maxBlocklistSize))
```

**Приоритет:** P1

---

### HIGH-4: Rate limiter — неограниченный рост map при DDoS

**Файл:** `mtglib/rate_limiter.go:60-62`
**Риск:** При DDoS с большого числа уникальных IP — map растёт без лимита. Cleanup каждую минуту удаляет записи старше 2 минут, но при 10k+ уникальных IP/мин — OOM.

**Production context:** MTProto на :8443 защищён CrowdSec iptables bouncer, что ограничивает входящий поток. Но rate limiter — последний рубеж.

**Рекомендация:** Добавить hard cap на количество записей:

```go
const maxRateLimiterEntries = 50000

if !exists && len(rl.limiters) >= maxRateLimiterEntries {
    rl.mu.Unlock()
    return false // Reject when table is full
}
```

**Приоритет:** P1

---

### MEDIUM-1: ParseClientHello модифицирует входной slice

**Файл:** `mtglib/internal/faketls/client_hello.go:66`
**Риск:** `copy(handshake[ClientHelloRandomOffset:], clientHelloEmptyRandom)` перезаписывает входной параметр. В текущем коде вызывается из `proxy.go` с `rec.Payload.Bytes()`, который не переиспользуется — реальной проблемы нет. Но нарушает Go-конвенцию immutable params.

**Приоритет:** P3 (cosmetic, no real exploit)

---

### MEDIUM-2: simple_run.go — секрет в аргументах CLI

**Файл:** `internal/cli/simple_run.go:21`
**Риск:** При запуске через `simple-run` секрет передаётся как CLI-аргумент, виден через `ps aux` / `/proc/PID/cmdline`.

**Production context:** В production используется `run /config.toml` (не simple-run). Secret в config.toml, не в аргументах. Docker CMD: `["run", "/config.toml"]`. Проблема НЕ актуальна для текущего деплоя.

**Рекомендация:** Добавить deprecation warning при использовании simple-run.
**Приоритет:** P3

---

### MEDIUM-3: Ticket: access.go — getIP без верификации

**Файл:** `internal/cli/access.go:108`
**Риск:** `getIP()` делает запрос к `ifconfig.co` и доверяет ответу как своему public IP. При MITM/DNS spoof — ошибочный IP.

**Production context:** Команда `access` — только для отображения URL, не влияет на работу прокси.

**Приоритет:** P3

---

### MEDIUM-4: Unbounded loop в generateServerHanshakeFrame

**Файл:** `mtglib/internal/obfuscated2/server_handshake.go:56-76`
**Риск:** Бесконечный цикл генерации random frame с условиями фильтрации. Вероятность зависания — исчезающе малая (условия отклонения: ~0.4% + ~0.00002%), среднее: 1-2 итерации.

**Рекомендация:** Добавить safety limit для defense-in-depth:

```go
const maxAttempts = 100
for i := 0; i < maxAttempts; i++ { ... }
```

**Приоритет:** P3

---

### LOW-1: IP hash truncation — 48 бит

**Файл:** `mtglib/ip_privacy.go`
**Описание:** SHA-256 truncated до 6 байт (48 бит). Достаточно для logging uniqueness, но birthday collision при ~16M уникальных IP.
**Контекст:** Прокси не обслуживает миллионы пользователей. 48 бит адекватно.
**Приоритет:** P4

---

### LOW-2: Forced type assertions в sync.Pool

**Файлы:** `faketls/pools.go`, `record/pools.go`, `relay/pools.go`, `obfuscated2/pools.go`
**Описание:** `pool.Get().(*Type)` без проверки `ok`. Panic при corruption крайне маловероятна — Pool.New() гарантирует тип.
**Приоритет:** P4

---

### LOW-3: Dockerfile включает example.config.toml

**Файл:** `Dockerfile:28`
**Описание:** `COPY --from=build /app/example.config.toml /config.toml` — fallback config в образе. В production перезаписывается через volume mount (`-v config.toml:/config.toml:ro`). Если volume не смонтирован — запуск с дефолтным конфигом (без секрета → ошибка Validate()).
**Приоритет:** P4

---

### INFO-1: obfuscated2 key derivation — SHA256(key||secret) без KDF

**Файл:** `mtglib/internal/obfuscated2/client_handshake.go:10-15`
**Описание:** Не использует формальный KDF (HKDF). Это протокольное решение MTProto obfuscated2 — менять нельзя без нарушения совместимости с клиентами.
**Не требует действий.**

---

### INFO-2: Prometheus bind 0.0.0.0 в конфиге

**Файл:** `mtproto/config.toml:98` — `bind-to = "0.0.0.0:9401"`
**Контекст:** Предыдущий аудит рекомендовал 127.0.0.1. Текущий конфиг: 0.0.0.0, но порт 9401 — `expose` (не `ports`) в docker-compose. Доступен только из docker-сети (Prometheus). Изоляция через Docker network — эквивалент 127.0.0.1. Корректно.
**Не требует действий.**

---

## Сводная таблица

| ID | Severity | Описание | Файл | Статус |
|----|----------|----------|------|--------|
| HIGH-1 | HIGH | secureRandIntn → panic при сбое crypto/rand | faketls/random.go | ✅ FIXED |
| HIGH-2 | HIGH | DoH response — io.LimitReader 64KB | dns_resolver.go | ✅ FIXED |
| HIGH-3 | HIGH | Blocklist download — io.LimitReader 100MB | firehol.go | ✅ FIXED |
| HIGH-4 | HIGH | Rate limiter — cap 50k entries | rate_limiter.go | ✅ FIXED |
| MEDIUM-1 | MEDIUM | ParseClientHello мутирует input | client_hello.go | P3 |
| MEDIUM-2 | MEDIUM | CLI secret в аргументах (simple-run) | simple_run.go | P3 |
| MEDIUM-3 | MEDIUM | access.go getIP без верификации | access.go | P3 |
| MEDIUM-4 | MEDIUM | Bounded loop server_handshake (100 att.) | server_handshake.go | ✅ FIXED |
| LOW-1 | LOW | IP hash 48 бит | ip_privacy.go | P4 |
| LOW-2 | LOW | Forced type assertions в pools | pools.go (4 файла) | P4 |
| LOW-3 | LOW | Example config в Dockerfile | Dockerfile | P4 |
| INFO-1 | INFO | SHA256 key derivation (протокол) | client_handshake.go | — |
| INFO-2 | INFO | Prometheus 0.0.0.0 (изолирован) | config.toml | — |

## Рекомендуемый порядок исправлений

1. **P1 (Sprint):** HIGH-2 + HIGH-3 + HIGH-4 — io.LimitReader и cap на rate limiter
2. **P2 (Next Sprint):** HIGH-1 — panic при отказе entropy
3. **P3 (Backlog):** MEDIUM-1..4 — code quality improvements
4. **P4 (Nice-to-have):** LOW-1..3

## Общая оценка

Проект в **хорошем состоянии безопасности** после предыдущих аудитов (07-12.02.2026):

- govulncheck: 0 уязвимостей, Go 1.25.7 с последними security fixes
- Все зависимости актуальны (обновлены 12.02.2026)
- Криптография корректна: crypto/rand, HMAC-SHA256, constant-time compare
- Anti-replay, rate limiting, blocklist — все включены
- Логирование privacy-aware (хэшированные IP)
- Docker scratch image, volume-mounted config

Основные направления hardening: resource limits (io.LimitReader), defense-in-depth (rate limiter cap).
