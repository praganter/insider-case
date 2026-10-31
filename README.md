# Smart Campaign Composer

**EN:** A marketer writes a campaign brief in plain language. Claude turns it into an audience rule and SMS/email copy, a human reviews and approves, messages go through a queue to a mock provider, and a dashboard shows delivery live. Bonuses: scheduling, A/B test, multi-language copy.

**TR:** Pazarlamacı kampanya brief'ini düz yazıyla yazar. Claude bunu kitle kuralına ve SMS/e-posta metnine çevirir, bir insan inceleyip onaylar, mesajlar kuyruktan sahte bir provider'a gider ve dashboard teslimatı canlı gösterir. Bonuslar: zamanlama, A/B testi, çok dilli metin.

## Run / Çalıştırma

```
cp .env.example .env      # leave ANTHROPIC_API_KEY empty for mock mode / mock mod için boş bırakın
docker compose up --build
```

- UI: http://localhost:3000 · API: http://localhost:8080 (`/healthz`, `/metrics`, `/mode`)
- **EN:** From your own tools (DBeaver, redis-cli): Postgres `localhost:5433`, Redis `localhost:6380`. Both listen on localhost only; user, database and ports come from `.env` (`POSTGRES_USER`, `POSTGRES_DB`, `POSTGRES_PORT`, `REDIS_PORT`; defaults in `.env.example`). Changing user or database after the first run needs `docker compose down -v` (it deletes the data).
- **TR:** Kendi araçlarınızdan (DBeaver, redis-cli): Postgres `localhost:5433`, Redis `localhost:6380`. İkisi de yalnızca localhost'u dinler; kullanıcı, veritabanı ve portlar `.env`'den gelir (`POSTGRES_USER`, `POSTGRES_DB`, `POSTGRES_PORT`, `REDIS_PORT`; varsayılanlar `.env.example` içinde). İlk çalıştırmadan sonra kullanıcı ya da veritabanı değiştirmek `docker compose down -v` ister (veriyi siler).
- **EN:** Empty `ANTHROPIC_API_KEY` = rule-based mock LLM (UI shows a MOCK badge); a key switches to the real model (`CLAUDE_MODEL`). With a key, API errors are shown with a retry; the app never falls back to mock silently.
- **TR:** Boş `ANTHROPIC_API_KEY` = kural tabanlı mock LLM (arayüzde MOCK rozeti); key varsa gerçek model kullanılır (`CLAUDE_MODEL`). Key varken API hataları tekrar dene seçeneğiyle gösterilir; mock'a sessizce düşülmez.

- **EN:** Default model is `claude-sonnet-5-5`. The mock LLM understands city, language (English), gender, age (ranges, over, under), cart_add / purchase / page_view and "last N days"; anything more nuanced needs a real key. The mock provider fails 5% and answers 429 for 5% of calls (`PROVIDER_ERROR_RATE`, `PROVIDER_TOO_MANY_RATE` in `.env`) so retries and the rate limit can be seen working.
- **TR:** Varsayılan model `claude-sonnet-5-5`. Mock LLM şehir, dil (İngilizce), cinsiyet, yaş (aralık, üstü, altı), cart_add / purchase / page_view ve "son N gün" ifadelerini anlar; daha ince ifadeler gerçek key ister. Mock provider çağrıların %5'inde hata, %5'inde 429 döner (`.env` içinde `PROVIDER_ERROR_RATE`, `PROVIDER_TOO_MANY_RATE`); böylece retry ve rate limit çalışırken görülebilir.

## Example briefs / Örnek brief'ler

**EN:** These work in mock mode and each finds a different audience (the first one is the case's own example). With a real key, free wording works too.

**TR:** Bunlar mock modda çalışır ve her biri farklı bir kitle bulur (ilki case'in kendi örneği). Gerçek key ile serbest ifade de çalışır.

1. `Son 30 günde sepetine ürün ekleyip alışveriş yapmayan İstanbul'daki kullanıcılara %30 indirim hatırlatması gönder. Samimi ama kısa olsun.` (city + cart_add + no purchase)
2. `Ankara'daki 25 yaş altı kadınlara yeni sezon ürünlerini duyur.` (city + gender + age < 25)
3. `Son 7 günde alışveriş yapan İzmir'deki erkeklere teşekkür mesajı gönder.` (city + gender + purchase)
4. `Sepetine ürün ekleyen 18-24 yaş arası kullanıcılara öğrenci indirimi duyur.` (age range + cart_add)
5. `Thank English-speaking women over 40 years old who made a purchase in the last 60 days.` (language + gender + age >= 40 + purchase)

## Architecture / Mimari

```
Vue UI (nginx) --> api (HTTP + SSE) --> Postgres
                       |                   ^
                       v                   |
                  Redis (Asynq) --> worker -+--> mockprovider (SMS / email)
                       ^
                  llm.Client (Anthropic or mock)
```

- **EN:** The API makes two LLM calls through the `llm.Client` interface: first a JSON segment rule (validated by Go, turned into parameterized SQL, never SQL from the LLM), then the streamed copy per channel x variant x language. After approval the API fans messages out (`UNIQUE(campaign_id, user_id, channel)`) into one Asynq queue per channel. The worker sends through a Redis token bucket (100/s per channel), retries with backoff, and a reconciler fails messages whose task was archived. The dashboard follows the campaign over SSE.
- **TR:** API, `llm.Client` arayüzü üzerinden iki LLM çağrısı yapar: önce JSON segment kuralı (Go doğrular, parametreli SQL'e çevirir; LLM'den SQL gelmez), sonra kanal x varyant x dil başına akan metin. Onaydan sonra API mesajları (`UNIQUE(campaign_id, user_id, channel)`) kanal başına bir Asynq kuyruğuna dağıtır. Worker Redis token bucket'ı (kanal başına 100/sn) üzerinden gönderir, backoff ile tekrar dener; reconciler görevi archive'a düşen mesajları `failed` yapar. Dashboard kampanyayı SSE ile izler.

API: `POST/GET /campaigns`, `GET /campaigns/{id}`, `PUT /campaigns/{id}/audience`, `POST|PUT /campaigns/{id}/content`, `POST /campaigns/{id}/approve|send|schedule|unschedule`, `GET /campaigns/{id}/events` (SSE), `POST /audience/suggest|preview`, `GET /mode`, `/healthz`, `/metrics`.

## Known limits / Bilinen sınırlar

- **EN:** `POST /audience/preview` is single-channel and not used by the UI. Backend error messages are English in the Turkish UI. The mock LLM matches keywords only, so a brief outside those patterns falls back to "active in the last N days".
- **TR:** `POST /audience/preview` tek kanallıdır ve arayüz kullanmaz. Backend hata mesajları Türkçe arayüzde İngilizcedir. Mock LLM yalnızca anahtar kelimeleri eşler; bu kalıpların dışındaki brief "son N günde aktif" kitlesine düşer.

## Tests / Testler

```
make test     # backend (Go + Postgres + Redis) and frontend (vitest), all inside Docker; no API key needed
```

**EN:** No local Go or Node needed. CI runs the same command. The real Anthropic client is tested against a fake HTTP server.

**TR:** Yerel Go ya da Node gerekmez. CI aynı komutu çalıştırır. Gerçek Anthropic istemcisi sahte bir HTTP sunucusuna karşı test edilir.

**EN:** Without `make` (e.g. Windows): run `docker compose run --rm --build test` and `docker compose run --rm --build test-frontend`.

**TR:** `make` yoksa (örn. Windows): `docker compose run --rm --build test` ve `docker compose run --rm --build test-frontend` komutlarını çalıştırın.

## Flow / Akış

**EN:**

1. Brief + channels (+ optional A/B) → audience suggestion with live count.
2. Copy per channel x variant x language, streamed, editable, with character counters.
3. Approve and send, or approve and schedule (cancel any time before it starts).
4. Live dashboard: pending / retrying / sent / failed, plus a per-variant table.

Nothing is sent before explicit approval.

**TR:**

1. Brief + kanallar (+ isteğe bağlı A/B) → canlı sayılı kitle önerisi.
2. Kanal x varyant x dil başına akan, düzenlenebilir metin ve karakter sayaçları.
3. Onayla ve gönder ya da onayla ve zamanla (başlamadan önce iptal edilebilir).
4. Canlı dashboard: bekleyen / yeniden denenen / gönderilen / başarısız, ve varyant tablosu.

Açık onay olmadan hiçbir şey gönderilmez.

## Structure / Yapı

```
backend/                 Go module / Go modülü
  cmd/api                HTTP API + SSE
  cmd/worker             queue worker, sends messages and starts scheduled campaigns / kuyruk worker'ı, mesajları gönderir, zamanlanmış kampanyaları başlatır
  cmd/mockprovider       fake SMS/email provider (errors, delays, 429) / sahte SMS/e-posta provider'ı (hata, gecikme, 429)
  cmd/seed               migrations + seed data / migration + örnek veri
  internal/segment       audience rule: validator and SQL builder / kitle kuralı: doğrulayıcı ve SQL builder
  internal/llm           Claude client, mock LLM, prompts / Claude istemcisi, mock LLM, prompt'lar
  internal/content       templates, character limits / şablonlar, karakter limitleri
  internal/campaign      campaign service and status machine / kampanya servisi ve durum makinesi
  internal/sender        queue tasks, retries, rate limit / kuyruk görevleri, yeniden deneme, hız limiti
  internal/store         Postgres queries / Postgres sorguları
  internal/testdb        throwaway test databases / tek kullanımlık test veritabanları
  migrations/            SQL files / SQL dosyaları
  scripts/               test runner used by make test / make test'in kullandığı betik
frontend/                Vue 3 + Vite + TypeScript (src/components, src/composables, src/lib.ts)
docs/                    plan.md, DECISIONS.md
ai-sessions/             sanitized Claude session exports / temizlenmiş Claude oturum kayıtları
.github/workflows/       CI
.githooks/               pre-commit leak check for session exports / oturum kayıtları için sızıntı kontrolü
.claude/                 project instructions and the ship-session skill / proje talimatları ve ship-session skill'i
docker-compose.yml       postgres, redis, seed, api, worker, mockprovider, frontend
Makefile                 make test / make up
.env.example             settings template / ayar şablonu
```

Design decisions: [docs/DECISIONS.md](docs/DECISIONS.md) · Plan: [docs/plan.md](docs/plan.md) · AI sessions: [ai-sessions/](ai-sessions/)
