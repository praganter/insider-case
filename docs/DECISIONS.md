# Decisions / Kararlar

Only the important decisions. Each heading is the problem; below it is what we decided and why. English and Turkish.
Sadece önemli kararlar. Her başlık problemi, altı alınan kararı ve nedenini gösterir. İngilizce ve Türkçe.

## 1. How do we keep LLM output away from the database? / LLM çıktısı veritabanından nasıl uzak tutulur?

- **EN:** The LLM returns only a JSON segment rule. Go validates it against whitelists (fields, operators, events, value types, nesting depth <= 3, <= 20 conditions) and builds parameterized SQL from fixed fragments; values always go through `$n` parameters. The LLM never writes SQL.
- **TR:** LLM sadece JSON segment kuralı döndürür. Go kuralı whitelist'lere göre doğrular (alan, operatör, olay, değer tipi, iç içe en fazla 3 seviye, en fazla 20 koşul) ve sabit SQL parçalarından parametreli sorgu üretir; değerler her zaman `$n` parametresiyle gider. LLM hiçbir zaman SQL yazmaz.

## 2. How do we get reliable JSON from Claude? / Claude'dan güvenilir JSON nasıl alınır?

- **EN:** Structured outputs (`output_config.format`) with a JSON schema; the current models reject forced `tool_choice`. The schema guarantees shape only (nested groups are unrolled to depth 3 because recursive schemas and length limits are unsupported), so Go enforces everything else: ranges, character limits, allowed variables.
- **TR:** JSON şemalı yapılandırılmış çıktı (`output_config.format`); güncel modeller zorunlu `tool_choice`'u reddediyor. Şema sadece biçimi garanti eder (özyinelemeli şema ve uzunluk sınırı desteklenmediği için iç içe gruplar 3 seviyeye açılır); aralıklar, karakter limitleri ve izinli değişkenler gibi geri kalan her şeyi Go uygular.

## 3. What happens when Claude is unavailable or answers badly? / Claude yoksa ya da kötü cevap verirse ne olur?

- **EN:** No key: the app runs on a built-in rule-based mock LLM and the UI shows a "MOCK" badge. Key present but the API fails: the user sees the error and can retry, we never fall back to mock silently (fake copy could be approved as real). Refusal or `max_tokens`: treated as failure. Valid JSON with an invalid rule: one repair turn with the validation errors, then the errors are shown and manual editing is possible.
- **TR:** Key yok: uygulama yerleşik kural tabanlı mock LLM ile çalışır, arayüzde "MOCK" rozeti görünür. Key var ama API hata veriyor: kullanıcı hatayı görür ve tekrar dener; sessizce mock'a düşmeyiz (sahte metin gerçek sanılıp onaylanabilir). Refusal ya da `max_tokens`: başarısızlık sayılır. JSON geçerli ama kural geçersiz: doğrulama hatalarıyla bir düzeltme turu, sonra hatalar gösterilir ve elle düzenleme mümkündür.

## 4. How do we test without an API key? / API key olmadan nasıl test ederiz?

- **EN:** All LLM access goes through one interface. Tests use the mock, and the real Anthropic client is tested against a fake HTTP server that imitates stream events, refusal, `max_tokens`, and 429/529. CI needs no key.
- **TR:** Tüm LLM erişimi tek bir arayüzden geçer. Testler mock'u kullanır; gerçek Anthropic istemcisi ise stream event'lerini, refusal'ı, `max_tokens`'ı ve 429/529'u taklit eden sahte bir HTTP sunucusuna karşı test edilir. CI için key gerekmez.

## 5. How do we express "added to cart but did not buy"? / "Sepete ekledi ama almadı" nasıl ifade edilir?

- **EN:** Event-based rules: user fields plus "event happened / did not happen within N days" (cart_add, purchase, page_view; 1-365 days), combinable with nested AND/OR. Channel consent is always added by the SQL builder, whatever the rule says.
- **TR:** Olay tabanlı kurallar: kullanıcı alanları ve "son N günde olay oldu / olmadı" (cart_add, purchase, page_view; 1-365 gün), iç içe AND/OR ile birleştirilebilir. Kanal izni, kural ne derse desin SQL builder tarafından her zaman eklenir.

## 6. How do we make sure nobody gets the same message twice? / Aynı kişiye aynı mesaj iki kez gitmesi nasıl engellenir?

- **EN:** `UNIQUE(campaign_id, user_id, channel)` in Postgres plus an idempotency key sent to the provider. The guarantee comes from the database, so it holds when a worker crashes or a task is enqueued twice.
- **TR:** Postgres'te `UNIQUE(campaign_id, user_id, channel)` ve provider'a gönderilen idempotency key. Garanti veritabanından geldiği için worker çökse ya da görev iki kez kuyruğa girse de geçerlidir.

## 7. How do we handle failures and the 100 msg/s limit? / Hatalar ve saniyede 100 mesaj limiti nasıl yönetilir?

- **EN:** Asynq queue on Redis. Retry on 429, 5xx, and timeouts with exponential backoff and jitter, up to 5 attempts, honoring `Retry-After`; other 4xx are not retried. After the last attempt the message is `failed` and stays in the Asynq archive, visible on the dashboard. The rate limit is a token bucket per channel in Redis, so it holds with any number of workers.
- **TR:** Redis üzerinde Asynq kuyruğu. 429, 5xx ve zaman aşımında üstel bekleme + jitter ile en fazla 5 deneme, `Retry-After`'a uyulur; diğer 4xx tekrar denenmez. Son denemeden sonra mesaj `failed` olur, Asynq archive'ında kalır ve dashboard'da görünür. Rate limit Redis'te kanal başına token bucket'tır; kaç worker çalışırsa çalışsın korunur.

## 8. How do character limits work with personalization? / Kişiselleştirmeyle karakter limitleri nasıl işler?

- **EN:** SMS <= 160 and email subject <= 80, counted in characters (runes), personalization values included. At approval the copy is rendered with the longest value of each variable in the audience; if it exceeds a limit, approval is blocked and the number of affected people is shown. Sending re-checks per person. The GSM-7/UCS-2 rule (70 characters with Turkish letters) is not applied: the case states 160 and most customers send abroad.
- **TR:** SMS <= 160, email konusu <= 80; karakter (rune) olarak sayılır, kişiselleştirme değerleri dahildir. Onayda metin, her değişkenin kitledeki en uzun değeriyle render edilir; limit aşılırsa onay engellenir ve etkilenen kişi sayısı gösterilir. Gönderimde kişi bazlı tekrar kontrol yapılır. GSM-7/UCS-2 kuralı (Türkçe harflerde 70 karakter) uygulanmaz: case 160 diyor ve müşterilerin çoğu yurtdışına gönderiyor.

## 9. Why two LLM calls and SSE? / Neden iki LLM çağrısı ve SSE?

- **EN:** Call 1 produces the segment rule so the marketer sees the audience size before any copy is written; call 2 streams the content. Both streams and the dashboard use Server-Sent Events: one direction is enough and it needs no extra library.
- **TR:** 1. çağrı segment kuralını üretir, böylece pazarlamacı metin yazılmadan önce kitle sayısını görür; 2. çağrı içeriği stream eder. İçerik akışı ve dashboard SSE kullanır: tek yön yeterli ve ek kütüphane gerekmez.

## 10. Which stack? / Hangi teknoloji?

- **EN:** Go backend (workers, concurrency, and SSE come naturally), Postgres for data, Redis + Asynq for the queue (retries, scheduled tasks for the scheduling bonus, and an archive as dead-letter queue come built in), Vue 3 + Vite + TypeScript for the UI.
- **TR:** Go backend (worker, eşzamanlılık ve SSE doğal gelir), veri için Postgres, kuyruk için Redis + Asynq (retry, zamanlama bonusu için zamanlanmış görev ve ölü mesaj kuyruğu olarak archive hazır gelir), arayüz için Vue 3 + Vite + TypeScript.

## 11. How does scheduling work, and what if a campaign is cancelled? / Zamanlama nasıl çalışır, iptal edilirse ne olur?

- **EN:** Scheduling is approval with a send time: the same checks as Approve (limits, empty audience), then an Asynq task with `ProcessAt` and status `scheduled`. The task is queued first and the status changed second, so a failure in between leaves a harmless task instead of a lost send. When the task fires it only starts campaigns that are still `scheduled` and due; cancelling (back to `content_ready`, needs re-approval) or rescheduling simply makes the old task a no-op, so we never delete tasks from Redis. Manual send of a scheduled campaign is rejected.
- **TR:** Zamanlama, gönderim zamanı olan bir onaydır: Approve ile aynı kontroller (limitler, boş kitle), sonra `ProcessAt` ile Asynq görevi ve `scheduled` durumu. Önce görev kuyruğa girer, sonra durum değişir; arada hata olursa kaybolan bir gönderim yerine zararsız bir görev kalır. Görev tetiklendiğinde yalnızca hâlâ `scheduled` olan ve zamanı gelmiş kampanyayı başlatır; iptal (`content_ready`'ye dönüş, yeniden onay gerekir) ya da yeniden zamanlama eski görevi etkisiz bırakır, bu yüzden Redis'ten görev silmeyiz. Zamanlanmış kampanyada elle gönderim reddedilir.

## 12. How is the A/B split made? / A/B ayrımı nasıl yapılır?

- **EN:** The campaign has an `ab_test` flag. At fan-out each recipient gets variant A or B from `hashtext(user_id) & 1` in the INSERT itself: stable (same person, same group), no extra state, roughly 50/50. `& 1` is used instead of `abs()` because `hashtext` can return the smallest int, where `abs()` overflows. The split is per user, not per campaign. Results are message counts per variant and status (campaign view and the live stream).
- **TR:** Kampanyada `ab_test` bayrağı var. Fan-out sırasında her alıcı, INSERT içinde `hashtext(user_id) & 1` ile A ya da B varyantını alır: kararlı (aynı kişi hep aynı grup), ek durum gerekmez, yaklaşık %50/%50. `abs()` yerine `& 1` kullanılır, çünkü `hashtext` en küçük int'i döndürebilir ve `abs()` taşar. Ayrım kullanıcı bazlıdır, kampanya bazlı değildir. Sonuçlar varyant ve durum başına mesaj sayısıdır (kampanya görünümü ve canlı akış).

## 13. How do languages and variants fit into content and limit checks? / Diller ve varyantlar içerik ve limit kontrolüne nasıl sığar?

- **EN:** A campaign needs one message per slot: channel x variant x language, where the languages are the ones its audience actually speaks (per channel, because consent differs). The LLM is given the exact slot list and must return each exactly once, otherwise the repair turn applies. The worst-case limit check uses the longest names of that language's audience only, so one very long Turkish name does not constrain English copy. At send time the worker picks the message by the user's own language and variant.
- **TR:** Bir kampanya slot başına bir mesaj ister: kanal x varyant x dil; diller kitlenin gerçekten konuştuğu dillerdir (izin kanala göre değiştiği için kanal bazında). LLM'e tam slot listesi verilir ve her birini tam bir kez döndürmelidir, aksi halde düzeltme turu devreye girer. En kötü durum limit kontrolü yalnızca o dilin kitlesindeki en uzun isimleri kullanır; çok uzun bir Türkçe isim İngilizce metni kısıtlamaz. Gönderimde worker mesajı kullanıcının kendi dili ve varyantıyla seçer.

## 14. How are database migrations versioned, and what does CI check? / Migration'lar nasıl versiyonlanır, CI neyi kontrol eder?

- **EN:** goose (the Go counterpart of Flyway) applies the embedded SQL files and records them in `goose_db_version`, so each file runs once; a Postgres session lock keeps api, worker, and seed from migrating at the same time. We chose a library over a hand-written runner because checksum-free version tracking and locking are easy to get subtly wrong. The existing files stay idempotent, so databases created before goose adopt the history on the first run. Down migrations are skipped: roll-forward is enough here. CI runs `gofmt` and `golangci-lint` (vet, staticcheck, errcheck), a `go mod tidy` drift check, `govulncheck`, and the tests with `-race` against real Postgres and Redis. Unactionable errcheck cases (deferred `Close`/`Rollback`, response writes) are excluded in `.golangci.yml`.
- **TR:** goose (Flyway'in Go karşılığı) gömülü SQL dosyalarını uygular ve `goose_db_version` tablosuna kaydeder; böylece her dosya bir kez çalışır. Postgres session lock'u api, worker ve seed'in aynı anda migration yapmasını engeller. Elle yazılmış bir runner yerine kütüphane seçtik; versiyon takibi ve kilitleme kolayca yanlış yazılır. Mevcut dosyalar idempotent kalır, bu yüzden goose'dan önce oluşmuş veritabanları geçmişi ilk çalışmada devralır. Down migration yok: burada ileri doğru ilerlemek yeterli. CI `gofmt` ve `golangci-lint` (vet, staticcheck, errcheck), `go mod tidy` sapma kontrolü, `govulncheck` ve gerçek Postgres ile Redis'e karşı `-race` ile testleri çalıştırır. Aksiyon alınamayan errcheck durumları (defer edilen `Close`/`Rollback`, response yazımları) `.golangci.yml`'de hariç tutulur.
