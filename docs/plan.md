# Smart Campaign Composer — uygulama planı

## Context
Insider One "AI Software Engineer" case'i (7 gün). Pazarlamacı brief'i → Claude → segment kuralı + SMS/email → insan onayı → kuyruk → mock provider → canlı dashboard.
Kaynak: Insider One'dan gelen mail + https://claude.ai/artifact/Vdtq1yNTx4buTo9A1jTvmj.
Puanlama: LLM %25, Mimari %20, Backend güvenilirlik %20, Frontend %20, Test %15. Dört bonus da yapılacak (çekirdek bittikten sonra; CI ilk gün).

## Alınan kararlar (2026-10-29)
| # | Karar | Gerekçe (DECISIONS.md'ye) |
|---|---|---|
| D1 | Backend Go | Worker/concurrency/SSE doğal |
| D2 | Postgres + Redis/Asynq | UNIQUE ile idempotency; Asynq retry, scheduled task, archive(DLQ) hazır |
| D3 | SMS ≤160 / konu ≤80 **isim dahil**, rune sayısı; GSM-7/UCS-2 (70) uygulanmaz | Case metni; `utf8.RuneCountInString`, `len()` değil |
| D4 | Limit aşımında onay engellenir: kitledeki en uzun değişken değeriyle render kontrol + gönderimde kişi bazlı kontrol | Sessiz kitle kaybı yok |
| D5 | İki adımlı LLM: kitle (JSON) → kullanıcı onayı → içerik (stream) | Adımlar ayrı doğrulanır/test edilir |
| D6 | SSE (LLM stream + dashboard) | Tek yön yeter; stdlib + EventSource |
| D7 | Dedup: UNIQUE(campaign_id, user_id, channel) + provider idempotency key | DB garantisi, worker crash'e dayanıklı |
| D8 | Retry: 429/5xx/timeout → exp backoff + jitter, max 5, Retry-After'a uy; diğer 4xx retry yok; bitince `failed` + Asynq archive | |
| D9 | Rate limit: Redis'te kanal başına paylaşılan token bucket (100/s) | Worker sayısından bağımsız |
| D10 | Auth kapsam dışı (README) | |
| D11 | Olay tabanlı segment DSL; SQL'i Go builder üretir: alan/olay adı → whitelist map'teki sabit SQL parçası, değerler `$n` parametresi. LLM yalnız JSON üretir | "LLM çıktısı SQL olarak çalışmaz" |
| D12 | İç içe AND/OR grupları; varsayılan sınır: derinlik ≤3, toplam koşul ≤20 | Esneklik; sınır = validator/DoS güvencesi |

| D13 | Structured output = **`output_config.format` (JSON schema, constrained decoding)**, forced `tool_choice` değil | Sonnet 5.5 / Opus 5.5 forced tool_choice'u 400 ile reddeder. Structured outputs streaming ile çalışır |
| D14 | Şema şekli garanti eder, **kuralları Go validator** uygular | Structured outputs recursive şema, min/max, minLength/maxLength desteklemez. Derinlik 3 şemada `$defs` ile açılır (group3→group2→group1→leaf) |

| D15 | **Mock LLM birinci sınıf çalışma modu**, sadece test aracı değil: case "geliştirme mock ile, gerçek key isteğe bağlı" diyor; değerlendirici de key'siz çalıştırabilir. Kural tabanlı: brief'ten şehir, "son N gün", sepet/satın alma, %X, ton kalıplarını yakalar → geçerli segment kuralı + şablon SMS/email; içeriği gecikmeli parça parça stream eder; tanımadığı brief'te güvenli varsayılan kural. UI'da "MOCK" rozeti | Key'siz `docker compose up` ile tam demo |
| D16 | Gerçek Anthropic entegrasyonu key'siz test edilir: `httptest` ile sahte Anthropic sunucusu, SDK `option.WithBaseURL` ile ona yönlenir; gerçek API cevap formatları (stream event'leri, refusal, max_tokens, 529) taklit edilir | LLM %25; entegrasyon kodu key olmadan doğrulanır |
| D17 | Key varken API hatasında mock'a **düşülmez**; kullanıcıya hata + tekrar dene | Sahte içerik gerçek gibi onaylanmasın |

Varsayılanlar: resmi `anthropic-sdk-go`, model `CLAUDE_MODEL` env'den (varsayılan `claude-sonnet-5-5`); Vue 3 + Vite + TS, UI kütüphanesi yok/hafif.
Kullanıcının key'i var: geliştirme mock ile, sonda kullanıcı gerçek key ile kısa manuel deneme yapar (Claude `.env`'i okumaz).

## LLM hata senaryoları (Claude yoksa / düzgün dönmezse)
| Senaryo | Davranış |
|---|---|
| Key yok (`ANTHROPIC_API_KEY` boş) | `LLM_MODE=mock`. API + UI'da "MOCK" rozeti, log'da uyarı. Testler ve CI hep bu modda |
| Key var, 429 / 529 / 5xx / bağlantı hatası | SDK'nın kendi retry'ı (max 2, backoff). Sonra kullanıcıya "LLM şu an ulaşılamıyor, tekrar dene" + kampanya taslakta kalır. **Mock'a sessizce düşülmez** (sahte içerik gerçek gibi onaylanmasın) |
| Timeout | `option.WithRequestTimeout` ile sınır (kitle ~30s, içerik stream'i ~90s); aşılırsa yukarıdaki gibi hata |
| `stop_reason = refusal` | İçerik okunmaz, `stop_details.category` log'lanır, kullanıcıya "brief işlenemedi, yeniden yaz" |
| `stop_reason = max_tokens` | Çıktı eksik sayılır → bir kez daha denenir, olmazsa hata |
| JSON şemaya uyuyor ama kural geçersiz (bilinmeyen alan, within_days 999, SMS 170 karakter) | Validator hata listesiyle **bir repair turu**; yine geçersizse kullanıcıya hatalar gösterilir, elle düzenleme mümkün |
| Kural geçerli ama 0 kişi | Hata değil; UI uyarır, onay butonu kapalı |

Her satır için mock LLM'de bir senaryo + bir test olur (mock: geçerli, geçersiz→repair, refusal, max_tokens, API hatası, yavaş stream). Mock aynı `llm.Client` arayüzünü uygular; seçim sadece `cmd/api` başlangıcında yapılır.

## Segment DSL
```json
{"match":"all","conditions":[
  {"field":"city","op":"eq","value":"İstanbul"},
  {"event":"cart_add","within_days":30,"exists":true},
  {"event":"purchase","within_days":30,"exists":false},
  {"match":"any","conditions":[ ... ]}
]}
```
- field whitelist: city, language, gender, age, created_at — op: eq, neq, in, gt, gte, lt, lte (alan tipine göre izinli op seti)
- event whitelist: cart_add, purchase, page_view — within_days 1..365
- Kanal izni kuraldan bağımsız, builder her zaman ekler (`consent_sms` / `consent_email`).

## Veri modeli (Postgres)
- `users(id, first_name, last_name, email, phone, city, language, gender, birth_year, consent_sms, consent_email, created_at)`
- `events(id, user_id, type, at, amount)` — index (user_id, type, at)
- `campaigns(id, brief, status, segment_rule jsonb, scheduled_at, created_at, approved_at)`
  status: draft → audience_ready → content_ready → approved|scheduled → sending → completed
- `campaign_contents(campaign_id, channel, variant, language, subject, body)`
- `messages(id, campaign_id, user_id, channel, variant, language, status, attempts, last_error, provider_msg_id, updated_at, UNIQUE(campaign_id,user_id,channel))`
- Seed: ~5000 user, gerçekçi dağılım (şehir, dil tr/en, izin oranları, son 90 günde olaylar), sabit random seed → test edilebilir sayılar.

## Repo yapısı
```
backend/            Go modülü (tek go.mod)
  cmd/api           HTTP + SSE
  cmd/worker        Asynq worker + scheduler
  cmd/mockprovider  sahte SMS/email servisi (hata/gecikme/429 oranları env'den)
  cmd/seed          migration + seed (idempotent)
  internal/segment  DSL tipleri, validator, SQL builder
  internal/llm      Client arayüzü, anthropic impl, mock impl, prompt'lar
  internal/content  render ({{first_name}}), limit kontrolü
  internal/campaign servis + durum makinesi
  internal/sender   task handler, retry sınıflandırma, ratelimit (Redis)
  internal/store    pgx sorguları
  migrations/
frontend/           Vue 3 + Vite
docker-compose.yml  postgres, redis, seed(one-shot), api, worker, mockprovider, frontend
Makefile            test → docker compose ile go test ./... + vitest (Windows'ta make/go gerekmeden)
.github/workflows/ci.yml
.env.example  README.md  DECISIONS.md  ai-sessions/
```
`ANTHROPIC_API_KEY` boşsa `LLM_MODE=mock` otomatik → `docker compose up` key'siz de çalışır.

## LLM güvenilirliği (en çok puanlanan kısım)
- Her çağrı `output_config.format` JSON şemasıyla; dönen JSON Go'da unmarshal + validator. `stop_reason` her zaman önce kontrol edilir.
- Geçersizse: hata listesi ile tek "repair" turu, yine geçersizse kullanıcıya anlamlı hata (bkz. "LLM hata senaryoları").
- İçerik akışı: text delta'ları (JSON parçaları) SSE ile frontend'e; bitince final obje doğrulanır (limit, değişken whitelist `{{first_name}}`, `{{last_name}}`, `{{city}}`), ihlaller UI'da gösterilir.
- Prompt'larda şema, whitelist ve bugünün tarihi; Türkçe brief → Türkçe içerik (dil bonusunda tr+en).
- Mock LLM: brief'e göre fixture + "önce bozuk sonra düzgün" senaryosu (repair testi).

## Aşamalar
-1. CLAUDE.md düzeltmesi: "forced tool use" → "structured outputs (`output_config.format`) + Go validator"; Hard rule 2 genişler: mock LLM birinci sınıf mod (D15), Anthropic entegrasyonu sahte sunucuyla test edilir (D16), key varken hatada mock'a düşülmez (D17); docs/plan.md bu planla güncellenir
0. İskelet: compose, Makefile, CI, healthz → verify: `docker compose up` + CI yeşil
1. Migration + seed + segment validator/builder + `POST /audience/preview` (count) → verify: tablo-testleri, injection testi, bilinen seed sayısı
2. LLM arayüzü + kural tabanlı mock + anthropic impl; brief → segment → validate/repair → verify: mock birim testleri, sahte Anthropic sunucusu testleri (D16); en sonda kullanıcı gerçek key ile manuel dener
3. İçerik üretimi (SSE) + render/limit kontrolü + düzenleme + onay → verify: 161/81 karakter, uzun isim engeli testleri
4. Gönderim: fan-out (INSERT ... ON CONFLICT DO NOTHING) → Asynq → rate limit → mock provider → retry/archive → verify: duplicate enqueue, provider 429/5xx, 1000 mesaj ≥10 sn testleri
5. Takip: SSE dashboard akışı, `/metrics` (Prometheus), `/healthz` (DB+Redis) → verify: curl
6. Frontend: brief → kitle önizleme (kural + sayı) → stream içerik + düzenleme + limit sayaçları → onay → canlı dashboard
7. Bonus: zamanlama (Asynq ProcessAt, iptal), A/B (`hash(user_id)%2`, varyant metrikleri), dil (user.language, dil×varyant×kanal limit kontrolü)
8. Dokümantasyon + ai-sessions temizliği (e-posta, yerel dosya yolları, key) + temiz klon testi

## Doğrulama (uçtan uca)
- Temiz klon → `cp .env.example .env` → `docker compose up` → UI'da örnek brief → kitle sayısı → stream içerik → onay → dashboard'da sayılar canlı akar, failed/retry görünür.
- `make test` key'siz yeşil; CI aynı komutu çalıştırır.
- `SELECT campaign_id,user_id,channel,count(*) ... HAVING count(*)>1` boş; mock provider loglarında saniyede >100/kanal yok.
