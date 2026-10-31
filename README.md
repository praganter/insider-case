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
frontend/                Vue 3 + Vite + TypeScript (src/App.vue, src/lib.ts)
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
