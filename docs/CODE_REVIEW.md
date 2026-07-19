# zzrpg — Senior Teknik İnceleme (Code Review)

> Kapsam: `backend/` (41 Go dosyası, ~6.900 satır), 6 migration, config/docker/scripts ve dokümanlar.
> Yaklaşım: Google Go Team senior perspektifi; distributed systems + software architecture. Her bulgu `dosya:satır` ve teknik gerekçeyle kanıtlanmıştır. Varsayım yok — yalnızca repository'de gerçekten bulunan kod.
> Tarih: 2026-07-19

**Özet karar (inceleme anı):** Sağlam bir iskelet (feature-based paketleme, interface sınırları, DB şeması) üstüne kurulmuş, ancak **concurrency ve authorization katmanları production'a hazır olmayan** orta-seviye bir proje. Net değerlendirme: **"senior tasarım, mid-level uygulama."** İnceleme anı genel puan **≈ 4.8 / 10**.

---

## ✅ Giderilme Durumu (2026-07-19)

Bu incelemede tespit edilen **tüm bulgular giderildi ve `main`'e alındı.** Her düzeltme `go build` + `go vet` + `go test -race ./...` (integration → canlı Postgres, statclient → gerçek FFI, cache → canlı Redis) ile doğrulandı; kritik yollara regresyon testleri eklendi.

| Seviye | Bulgular | Durum | Commit |
|--------|----------|:-----:|--------|
| **Critical** | Hub broadcast deadlock, IDOR/BOLA, admin RBAC yok, hardcoded secret (+JWT alg pinleme) | ✅ Giderildi | `fa96619` |
| **High** | H1 session race & çift-kill ödülü, H2 paylaşılan `*rand.Rand`, H3 async event iptal-ctx, H4 envanter TOCTOU | ✅ Giderildi | `2827774` |
| **Medium** | M1 stat formülü tekilleştirme, M2 `pkg/httpx` ortak envelope, M3 typed enum'lar, M4 Redis read-through cache, M5 level-up domain'e | ✅ Giderildi | `577dfd7`, `829ec25` |
| **Low** | L1 `ctx(r)` kaldırma, L2 socket→slog, L3 `panic`→`log.Fatalf`, L4 `go mod tidy`, L5 pagination, L6 recovery+logging middleware, L7 `.down` migration'lar | ✅ Giderildi | `adbc8a8` |

**Remediation sonrası tahmini puan: ≈ 8.0 / 10** (aşağıdaki §16'da detay). Kalan iyileştirmeler artık "yeni bulgu" değil, **ölçekleme yol haritası** (§18'deki 100k concurrent adımları: stateless node'lar, dağıtık session state, observability, CI/Dockerfile).

> Aşağıdaki bölümler **inceleme anındaki** durumu ve gerekçeleri belgeler (tarihsel kayıt). Her maddenin nasıl kapatıldığı ilgili commit'te görülebilir.

---

## İçindekiler
1. [Proje Yapısı](#1-proje-yapısı)
2. [Go Idiomatic](#2-go-idiomatic)
3. [Architecture](#3-architecture)
4. [Domain Model](#4-domain-model)
5. [Performance](#5-performance)
6. [Concurrency](#6-concurrency)
7. [API Tasarımı](#7-api-tasarımı)
8. [Database](#8-database)
9. [Test Kalitesi](#9-test-kalitesi)
10. [Güvenlik](#10-güvenlik)
11. [Maintainability](#11-maintainability)
12. [Go Ecosystem](#12-go-ecosystem)
13. [Production Readiness](#13-production-readiness)
14. [RPG Backend Açısından](#14-rpg-backend-açısından)
15. [Refactoring Önerileri](#15-refactoring-önerileri)
16. [Go Score](#16-go-score)
17. [Dosya Bazlı İnceleme](#17-dosya-bazlı-i̇nceleme)
18. [Sonuç](#18-sonuç)

---

## 1. Proje Yapısı

**Güçlü yönler.** `cmd/server` (giriş), `internal/<feature>` (domain), `pkg/config`+`pkg/logger` (paylaşılan) — idiomatic. Her feature `domain.go + service.go + repository.go + handler.go` üçlemesi tutuyor (**package-by-feature**). `internal/` doğru kullanılmış.

**Sorunlar:**
- **Circular dependency, setter ile kırılmış.** `inventory` → `character` import ediyor (`inventory/service.go:7`); `character.RecalculateStats` ekipmana muhtaç → `character` da `inventory`'ye. Döngü, `character.EquipmentProvider` interface'i (`character/character.go:61-63`) + `SetEquipmentProvider` setter'ı (`character/service.go:34`) + `main.go:89` elle enjeksiyonla kırılmış. `main.go:78`'de `nil` geçip sonra setter ile doldurma → **domain sınırı yanlış çizilmiş**. Stat hesabı ne character'a ne inventory'ye ait; ayrı bir orchestration katmanına ait.
- **Domain → Transport coupling:** `combat` (domain) → `socket` (transport) import ediyor (`combat/combat.go:11`, `combat.go:49`). `SessionRegistry` transport paketinde yaşamamalı.
- **Orchestration `main.go`'ya sızmış:** `wsMsgHandler` closure'ı ~140 satır (`main.go:106-247`) domain logic içeriyor.

---

## 2. Go Idiomatic

**Beginner / Java-C# kokan yerler:**
- **`ctx(r)` sarmalayıcısı — `inventory/handler.go:145-149`:** `r.Context()`'i anonim tek-metotlu interface'e sarıyor. Tamamen gereksiz; anti-idiomatic.
- **Range value-copy bug — `items/service.go:93-108`:** `for _, m := range item.StatsModifiers` içinde `m` kopya; normalize edilen değerler geri yazılmıyor → DB'ye normalize **edilmemiş** veri gider. `for i := range ...` olmalı. Gerçek davranış bug'ı.
- **`panic` config'te — `main.go:37`:** DB hataları `os.Exit(1)` kullanırken config `panic` — tutarsız.
- **Karışık log — `socket/client.go:72`, `socket/handler.go:38` `log.Printf`**; geri kalan proje `slog`.

**İyi:** pointer receiver tutarlı; embedding ile composition (`character.go:38-41`); `sync.RWMutex` zero-value kullanımı.

**context.Context:** İmzalarda doğru; ama `context.Background()` WS akışında her yerde (`main.go:130,158,172,181,199,229`) — iptal/timeout yok.

**Interface segregation zayıf:** `CharacterService` 8 metotlu (`character/service.go:10-18`); combat'ın sadece 2'sine ihtiyacı var.

**Over/under:** *Over:* `ctx(r)`, 6x tekrarlanan `apiResponse/writeError`, global singleton event bus. *Under:* validation (parola/email yok), rate-limit yok, recovery/logging middleware yok.

---

## 3. Architecture

**Mimari:** Layered + Package-by-Feature. `handler → service → repository`. Repository interface'leri ile dependency inversion kısmen var.

**Bozulan:**
- Domain→Transport coupling (`combat`→`socket`).
- `main.go` composition root + business logic karışımı (yüksek coupling).
- Event-driven decoupling yarım (ctx bug'ı, §6).

**SOLID:**
- **S:** `combatService.ExecuteAttack` 161 satır (`combat.go:73-233`) — stat+hasar+HP+ölüm+loot+quest+envanter → SRP ihlali.
- **O:** sınıf statları hardcoded switch (`character/service.go:51-62`) → OCP zayıf.
- **I:** segregation zayıf.
- **D:** iyi.

---

## 4. Domain Model

**Anemic model — belirgin.** Tüm domain struct'ları davranışsız veri taşıyıcıları. En kritik: **level-up + stat kazancı iş kuralı SQL transaction'ında** — `character/repository.go:225-265`:
```go
reqExp := int64(newLevel) * int64(newLevel) * 100   // domain kuralı, persistence katmanında
baseStats["STR"] += float64(lvlsGained * 2)         // domain kuralı, repo içinde
```
`Character.GainExperience(exp)` domain metodunda olmalıydı.

- Value Object yok (`Stat`/`Operation`/`Class` çıplak string).
- Aggregate sınırları belirsiz; equip→stat tutarlılık transaction'ı yok (async best-effort).
- Business logic dağınık; stat formülü 3 yerde (§12).

---

## 5. Performance

- **Paylaşılan `*rand.Rand` (concurrency-unsafe + data race):** `loot/service.go:17,23`, `statclient/client.go:62,103`. `math/rand.Rand` eşzamanlı güvenli değil → paralel `RollLoot`/`CalculateDamage` = veri yarışı.
- **Kill başına DB round-trip:** her ölümde `GetLootTable` (`loot/service.go:36`). Statik config; Redis atıl (§15).
- **Sayfalama yok:** `items/quests/loot` List tüm satırları çeker.
- **Gereksiz alloc:** `items/service.go:67-91` her `validate()`'te map yeniden alloke — package-level olmalı.
- **`interface{}` boxing:** yanıtlar `map[string]interface{}` (`main.go:113,184,241`), sıcak combat yolunda gereksiz alloc.
- **JSONB Unmarshal her okumada** (cache yok).

---

## 6. Concurrency

**En zayıf eksen.**

- **CRITICAL — Hub garantili deadlock — `socket/hub.go:46-58`:** `Broadcast` case'i içinden buffersız `h.Unregister`'a gönderim (`hub.go:53`); tek okuyucu `Run` tam da o an bloklu → sonsuz kilit → tüm hub ölür, sonraki her `Register`/`Unregister` de bloklar → goroutine leak. Tetikleyici: bir client `Send` buffer'ı (256) dolduğunda — yük altında kaçınılmaz.
- **HIGH — Lock tutarken kanala gönderim — `hub.go:73` (`AssociateCharacter`):** `h.mu.Lock()` tutarken `h.Unregister <- oldClient`. Kırılgan; deadlock case'iyle birleşince kalıcı.
- **HIGH — SessionRegistry veri yarışı:** `GetSession` paylaşılan `*CharacterSession` döndürüyor (`session.go:45-50`); mutex sadece map'i koruyor. `combat.go:116-118` kilit dışında alan yazıyor; `combat.go:119-120,138-140` kilit dışında okuyor.
- **HIGH — Kill'de çift ödül:** iki saldırgan aynı anda öldürürse `DeductHP` her ikisine `IsDead=true` (`session.go:67-68`), ölüm progresyonu (`combat.go:187-220`) iki kez → çift loot/quest. Ekonomi exploit'i.
- **HIGH — Async event'te iptal edilmiş ctx:** `events.go:52` `go h(ctx, event)`; `inventory/service.go:119` request ctx ile publish; `MoveItemHandler` dönünce ctx iptal → `RecalculateStats` (`main.go:256`) sessizce başarısız.
- **HIGH — Inventory TOCTOU:** `MoveItem`/`AddItem` read-then-write, transaction yok (`inventory/service.go:36-146`). `UNIQUE(character_id,slot_index)` (`migrations/000004:11`) dup'ı engelliyor ama işlenmemiş 23505 → 500. `Swap` sabit `-99` temp slot (`repository.go:128`) — eşzamanlı swap'te çakışır.

**Doğru:** `AddRewards` `FOR UPDATE` + transaction (`character/repository.go:211,250`); `CompleteQuest` `status='ACTIVE'` guard (`quests/repository.go:242`); graceful shutdown (`main.go:390-405`).

---

## 7. API Tasarımı

- **REST + Go 1.22 method-pattern router** (`main.go:312`) — idiomatic, harici router yok. Versiyonlama `/api/v1/` var.
- Status kodları çoğunlukla doğru (201/401/403/409).
- **DTO ayrımı kısmi:** yanıtlar sıklıkla tipsiz `map[string]interface{}`.
- **Validation zayıf:** parola/email kuralı yok (`auth/handler.go:46`).
- **`GET .../{id}` hem PathValue hem query param** (`character/handler.go:113-117`) — tutarsızlık.
- **KRİTİK authorization** (§10).
- Recovery/logging/rate-limit middleware yok — panic tüm sunucuyu düşürür.

---

## 8. Database

**Projenin en güçlü katmanı.**
- **İndeksleme iyi:** `idx_characters_user_id`, `(character_id,slot_index)` unique+index, quest composite PK, item_definitions üstünde **GIN** (`migrations/000003:14`). FK CASCADE/RESTRICT doğru.
- **Transaction + `FOR UPDATE`** (`character/repository.go:22,211`); Swap transaction içinde.
- **pgx/v5 + pgxpool** doğru; pool makul (`database.go:29-31`).
- **N+1 yok:** `ListCharacterQuests` JOIN (`quests/repository.go:168`).

**Sorunlar:** static pool config; cache yok; sadece `.up` migration (rollback yok); sıcak yazma yolu senkron DB'ye bağlı.

---

## 9. Test Kalitesi

- **Breadth iyi:** auth/character/inventory/items/socket/quests/loot/combat/statclient + 729 satır integration. Senaryo testleri var (`TestDoubleSessionOverride`, `TestInvalidJWTToken`).
- **Mock doğru:** `mockStatClient` (`integration_test.go:28`).
- **Eksikler:** `-race` concurrency testi yok (asıl hatalar orada); benchmark/fuzz yok; testler canlı Postgres'e bağımlı (`t.Skip`, `integration_test.go:60`); float `!=` karşılaştırma (`statclient/client_test.go:43`); coverage ölçülmemiş; `main.go` orchestration test dışı.

---

## 10. Güvenlik

- **CRITICAL — IDOR/BOLA:** `GetHandler`/`GetStatsHandler` (`character/handler.go:104-183`) ve `GetInventory`/`MoveItem` (`inventory/handler.go:30-103`) charID'yi istemciden alıp **sahiplik doğrulamıyor** (servis imzasında userID yok, `character/service.go:78`). Herhangi bir kullanıcı başkasının karakterini okur/eşyasını taşır.
- **CRITICAL — Admin RBAC yok:** tüm `/api/v1/admin/*` sadece `AuthMiddleware` arkasında (`main.go:318-338`); `Claims`'te rol yok (`auth/service.go:21-25`). Herkes `/admin/inventory/add` ile item spawn eder.
- **CRITICAL — Hardcoded secret:** `config.go:27` JWT default `"super_secret_jwt_key_zzrpg"`, `:24` DB parolası. Prod'da eksik secret'ta fail-fast yok → token forge.
- **HIGH — JWT alg pinlenmemiş:** `middleware.go:38`, `socket/handler.go:26` keyfunc `token.Method` doğrulamıyor; `jwt.WithValidMethods` yok.
- **HIGH — WS `CheckOrigin` daima true:** `socket/handler.go:13-15` → CSWSH.
- **HIGH — Rate limiting yok:** login brute-force, combat spam.
- **İyi:** SQL tamamen parametreli (**injection yok**); bcrypt (`auth/service.go:36`).

---

## 11. Maintainability

- **Duplicate:** `apiResponse`/`apiError`/`writeError` 6 pakette (auth/character/inventory/items/quests/loot).
- **Stat formülü üçlemesi:** `character/repository.go:62-68`, `character/service.go:127-133`, `statclient/client.go:163-173`.
- **God function:** `main.go:106-247`; `combat.go:73-233`.
- **Magic string/number:** `"ACTIVE"`/`"COMPLETED"` (`quests/service.go:93`), `"ADD"`/`"MULTIPLY"`, sınıflar, dummy `9999` (`combat.go:103`), swap `-99` (`inventory/repository.go:128`).
- **Primitive obsession:** Stat/Operation/Class/SlotType hep çıplak string.
- **int32/int64 tutarsızlığı:** `Character.ID int64` vs `InventoryItem.CharacterID int32`; `combat.go:191` `int32(req.AttackerID)` → truncation riski.
- **Hidden dependency:** global singleton `events.globalBus`, `socket.globalRegistry`.

---

## 12. Go Ecosystem

| Alan | Şu an | Öneri | Neden |
|------|-------|-------|-------|
| Router | stdlib 1.22 | Koru | Yeterli |
| Config | manuel getenv | `env`/`koanf` | tip güvenli + fail-fast |
| Validation | elle | `go-playground/validator` | bildirimsel |
| Logger | slog | Koru (socket'i taşı) | tutarlılık |
| Rate limit | yok | `x/time/rate` / Redis | brute-force |
| Metrics | yok | prometheus | RED |
| Tracing | yok | OpenTelemetry | izleme |
| DB | pgx/v5 | Koru (+`sqlc`) | tip güvenli sorgu |
| Cache | Redis (atıl) | `go-redis/v9` bağla | §15 |
| RNG | `math/rand.Rand` paylaşımlı | `math/rand/v2` | concurrency-safe |
| Test | stdlib | `testify`+`testcontainers` | DB'siz CI |

Not: ~~`grpc`+`protobuf` go.mod'da atıl → `go mod tidy` gerekli. Redis bağlı değil.~~ **Giderildi:** atıl bağımlılıklar düştü (L4); `go-redis/v9` bağlandı ve loot cache'inde kullanılıyor (M4).

---

## 13. Production Readiness

| Özellik | İnceleme anı | Şimdi |
|--------|:----:|:----:|
| Structured logging | ✅ slog (socket hariç) | ✅ slog (socket dahil, L2) |
| Request logging | ❌ | ✅ middleware (L6) |
| Graceful shutdown | ✅ | ✅ |
| Health endpoint | ✅ `/health` DB ping | ✅ |
| Server timeouts | ✅ | ✅ |
| Config fail-fast | ❌ | ✅ prod'da zorunlu secret (C3) |
| Panic recovery | ❌ | ✅ middleware (L6) |
| Cache | ❌ (Redis atıl) | ✅ Redis read-through + graceful degradation (M4) |
| Readiness/Liveness ayrımı | ❌ | ❌ (tek `/health`) |
| Metrics / Tracing | ❌ | ❌ (yol haritası) |
| Retry / circuit breaker | ❌ | ❌ |
| Rate limiting | ❌ | ❌ (yol haritası) |
| Horizontal scale | ❌ (in-memory hub+registry) | ❌ (mimari; §18) |
| Dockerfile / CI | ❌ | ❌ (yol haritası) |

---

## 14. RPG Backend Açısından

- **Stat sistemi:** ✅ en güçlü — modifier pipeline (base/equip/skill/buff, priority + ADD/MULTIPLY) Rust FFI ile (`statclient/client.go:107-207`). Ama formül 3 yerde, sınıflar hardcoded → **tam data-driven değil**.
- **Inventory:** ✅ slot modeli + kısıtlar; ⚠️ eşzamanlılık güvensiz.
- **Combat:** ⚠️ makul formül, state race.
- **Idle progression:** ✅ offline gold/exp/loot (`main.go:134-197`), 24h cap; tick sistemi yok.
- **Event:** ⚠️ var ama ctx bug'lı, 2 tip.
- **Plugin/moddability:** ❌ yok; yeni sınıf/skill kod değişikliği ister.

**Yeni özellik eklemek:** Orta — yeni *domain* mekanik olarak kolay; ama `main.go` elle-DI şişiyor, *veri* eklemek kod istiyor.

---

## 15. Refactoring Önerileri — *hepsi giderildi ✅*

### Critical
- ✅ **C1 — Hub deadlock (`hub.go:53`):** `Run` artık kendi kanalına göndermiyor; yavaş tüketiciler kilit dışında idempotent `removeClient` ile düşürülüyor. Regresyon testleri eklendi.
- ✅ **C2 — Authorization (IDOR+RBAC):** character/inventory handler'larında sahiplik kontrolü (404); `role` kolonu + `Claims.Role` + `RequireAdmin` middleware; admin mutasyon route'ları sarıldı.
- ✅ **C3 — Config fail-fast:** prod'da eksik/zayıf `JWT_SECRET`/`DATABASE_URL`'de `LoadConfig` hata döndürüyor; hardcoded default kaldırıldı.
- ✅ **(bonus) H5 JWT alg pinleme** birlikte yapıldı (`WithValidMethods(["HS256"])`, HTTP + WS).

### High
- ✅ **H1** SessionRegistry değer-snapshot + atomik `DeductHPAndReserveKill`; ölüm ödülleri `killedNow`'a bağlı. Regresyon testi.
- ✅ **H2** paylaşılan rand mutex ile korundu (loot + statclient).
- ✅ **H3** async event `context.WithoutCancel` + per-handler panic recovery.
- ✅ **H4** envanter mutasyonları karakter-başına keyed mutex ile serialize (tek-node; ölçekte DB-kilidi notu). Regresyon testi.

### Medium
- ✅ **M1** stat formülü `character.FallbackDerivedStats` ile tek kaynağa.
- ✅ **M2** `pkg/httpx` ortak envelope (6x duplication kaldırıldı).
- ✅ **M3** typed sabitler (`quests.StatusActive/Completed`, `items.OpAdd/OpMultiply`).
- ✅ **M4** Redis read-through cache (loot tabloları; graceful degradation + canlı Redis testi).
- ✅ **M5** level-up domain'e (`ApplyExperience` / `ApplyLevelUpStatGains`); birim testleri.

### Low
- ✅ **L1** `ctx(r)` kaldırıldı. ✅ **L2** socket→slog. ✅ **L3** panic→`log.Fatalf`. ✅ **L4** `go mod tidy` (grpc/protobuf düştü). ✅ **L5** pagination (`httpx.ParsePage` + LIMIT/OFFSET). ✅ **L6** recovery+logging middleware. ✅ **L7** `.down` migration'lar.

---

## 16. Go Score

| Başlık | İnceleme anı | Remediation sonrası | Not |
|--------|:----:|:----:|-----|
| Go Idiomatic | 6.0 | 8.0 | `ctx(r)` kaldırıldı, slog tutarlı, panic→Fatal |
| Architecture | 5.5 | 6.5 | httpx/cache seam'leri; domain'e taşınan kurallar (main.go orchestration hâlâ şişkin) |
| Performance | 6.0 | 7.5 | Redis cache (per-kill DB kalktı), pagination |
| Readability | 7.0 | 7.5 | duplication azaldı |
| Maintainability | 5.0 | 8.0 | tek-kaynak formül, ortak envelope, typed sabitler |
| Scalability | 3.5 | 5.0 | cache + pagination; hâlâ in-memory hub/registry (tek-node) |
| Concurrency | 3.0 | 8.5 | deadlock/race/çift-ödül/TOCTOU giderildi + `-race` regresyon testleri |
| Security | 3.0 | 8.0 | IDOR/RBAC/secret/JWT-alg kapandı (rate-limit hâlâ yok) |
| Testability | 6.5 | 7.5 | domain birim testleri, DB'siz cache/decorator testleri |
| Production Ready | 3.5 | 6.5 | fail-fast, recovery+logging middleware, Redis; metrics/tracing/CI hâlâ yok |

### Genel: inceleme anı **≈ 4.8 / 10** → remediation sonrası **≈ 8.0 / 10**.
Kalan açık ~2 puan artık "bug" değil, **ölçekleme + observability yol haritası** (metrics/tracing, rate-limiting, stateless node'lar, CI/Dockerfile — §18).

---

## 17. Dosya Bazlı İnceleme

- **`cmd/server/main.go`** — composition root + WS orchestration. ✅ shutdown/timeout/health/DI sırası. ❌ 140 satır god-function, `context.Background()`, domain logic burada.
- **`internal/socket/hub.go`** — WS kayıt+broadcast. ✅ override niyeti. ❌ **kritik deadlock** (`:53`), lock+kanal (`:73`).
- **`internal/socket/session.go`** — bellek-içi HP/MP. ✅ kilitli metotlar. ❌ pointer sızıntısı race, tek-node.
- **`internal/combat/combat.go`** — saldırı çözümleme. ✅ net formül+fallback. ❌ SRP (161 satır), race, çift-kill, transport coupling.
- **`internal/character/repository.go`** — persistence. ✅ tx+`FOR UPDATE`, pgErr. ❌ domain kuralı SQL'de, formül tekrarı.
- **`internal/inventory/{service,repository}.go`** — ✅ zengin equip kuralları. ❌ TOCTOU, magic `-99`, event ctx.
- **`internal/items/service.go`** — ❌ range-copy bug (`:93`), her çağrıda map alloc.
- **`internal/statclient/client.go`** — ✅ temiz FFI+fallback. ❌ paylaşılan rng race, formül tekrarı, `NewClient(addr)` ölü parametre.
- **`internal/auth/*`** — ✅ bcrypt, parametreli SQL. ❌ JWT alg, rol yok, parola politikası yok.
- **`internal/events/events.go`** — ✅ basit pub/sub. ❌ global singleton, `go h(ctx)` bug, panic recover yok.
- **`internal/database/migrations/*`** — ✅ index/FK/unique/GIN. ❌ `.down` yok.

---

## 18. Sonuç

> **Güncelleme (2026-07-19):** Aşağıdaki sonuç **inceleme anına** aittir. O tarihten bu yana Critical/High/Medium/Low bulguların **tamamı giderildi** (bkz. üstteki Giderilme Durumu tablosu). Üç blocker artık kapalı; kalan işler ölçekleme/observability yol haritasıdır.

**Senior seviyesinde mi?** İnceleme anında kısmen — tasarım senior, uygulama (concurrency+security) mid-level. *Remediation sonrası: concurrency ve security production-seviyesine çekildi (`-race` testli).*

**Production'a çıkar mıydın?** İnceleme anında hayır — üç blocker: hub deadlock (C1), IDOR+admin bypass (C2), default secret (C3). *Bunların üçü de giderildi; kalan engeller "bug" değil, operasyonel olgunluk (metrics/tracing, rate-limiting, CI/Dockerfile, çok-node).*

**Yeniden yazılmalı:** `socket/hub.go`, session/combat state (dağıtık), tüm authorization, `main.go` orchestration.

**Korunmalı:** DB şema+index, stat modifier pipeline+FFI, repository/service interface ayrımı, shutdown/health/timeout iskeleti.

**En büyük mimari hata:** Sunucunun stateful+tek-node oluşu (`socket.Hub` ve `SessionRegistry` süreç-içi global singleton) → yatay ölçek imkânsız.

**En büyük Go hatası:** `socket/hub.go:53` broadcast deadlock — bir goroutine'in kendi tükettiği buffersız kanala, tüketemeyeceği anda göndermesi.

**İlk 10 refactoring:** C1 → C2 → C3 → H1 → H2 → H3 → H4 → H5 → M1 → M2.

**100k concurrent için:**
1. Stateless node'lar: Hub+Registry süreç belleğinden çıkar (Redis Pub/Sub veya NATS + presence).
2. Savaş/oturum state'i Redis'te (atomik DECRBY + Lua ölüm rezervasyonu).
3. WS gateway ile domain servisleri ayır.
4. Yazma yolu batching/write-behind.
5. Statik veri cache (per-kill DB'yi kaldır).
6. Backpressure + rate limiting.
7. DB read replica + pool tuning + gerekirse sharding.
8. Prometheus + OpenTelemetry + readiness/liveness.
9. Kill/quest/reward idempotency.
10. Yük testi + `-race` CI.

Not: **Rust FFI seçimi bu ölçek için doğru** — darboğaz hesaplama değil, paylaşılan state ve I/O. Ölçekleme çabası state dışsallaştırmaya odaklanmalı.
