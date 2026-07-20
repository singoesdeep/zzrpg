# API Tasarımı: zzrpg REST, WebSockets ve FFI Arayüzleri (TR)

Bu belge; Frontend İstemcisi, Go Backend ve süreç-içi Rust `zzstat` motoru arasındaki arayüz özelliklerini tanımlar.

> **Motor güncellemesi.** Motor dönüşümünden bu yana auth yüzeyi kısa-ömürlü bir
> access token artı rotating bir refresh token döndürür ve yeni operasyonel
> endpoint'ler mevcuttur. Güncel tam endpoint listesi [README](../README.md)'de;
> mimari [ARCHITECTURE_TR](ARCHITECTURE_TR.md)'de.
>
> - `POST /api/v1/auth/login` → `{ token (access), refresh_token, expires_in }`
> - `POST /api/v1/auth/refresh` — refresh token'ı yeni bir çifte çevir (tek-kullanımlık).
> - `POST /api/v1/auth/logout` — refresh token'ı iptal et.
> - `GET /health` — liveness (DB ping). `GET /readyz` — readiness (DB hard, Redis soft).
> - `GET /metrics` — Prometheus metrikleri.
> - WebSocket'e sunucu → istemci `AWAY_EVENTS` paketi eklendi (girişte event-log replay).
> - Sertleştirme: IP-başı rate limit (429), `X-Request-ID` korelasyonu, güvenlik
>   başlıkları, istek gövde-boyut limiti, login brute-force lockout (429).

---

## 1. REST API Uç Noktaları (REST API Endpoints)

Tüm REST API istekleri JSON formatında yanıt döner. Standart formatlar aşağıdaki gibidir:
- Başarı (Success): `{ "success": true, "data": { ... } }`
- Hata (Error): `{ "success": false, "error": { "code": "ERROR_CODE", "message": "Human readable message" } }`

### 1.1 Kimlik Doğrulama ve Kayıt (Authentication & Registration)
- **POST `/api/v1/auth/register`**
  - İstek: `{"username": "player1", "email": "player1@rpg.com", "password": "secure_password"}`
  - Yanıt: `{"success": true, "data": {"user_id": 101}}`
- **POST `/api/v1/auth/login`**
  - İstek: `{"username": "player1", "password": "secure_password"}`
  - Yanıt: `{"success": true, "data": {"token": "jwt_token_here", "expires_in": 3600}}`

### 1.2 Karakter Yönetimi
- **GET `/api/v1/characters`**
  - Başlık (Header): `Authorization: Bearer <token>`
  - Yanıt: `{"success": true, "data": [{"id": 1, "name": "SuraKing", "class_name": "SURA", "level": 15, "gold": 12000}]}`
- **POST `/api/v1/characters`**
  - İstek: `{"name": "SuraKing", "class_name": "SURA"}`
  - Yanıt: `{"success": true, "data": {"id": 1, "name": "SuraKing", "level": 1, "class_name": "SURA"}}`
- **GET `/api/v1/characters/:id/stats`**
  - Yanıt:
    ```json
    {
      "success": true,
      "data": {
        "character_id": 1,
        "base_stats": {"STR": 15, "INT": 22, "DEX": 10, "CON": 12},
        "final_stats": {"HP": 1250, "MP": 820, "ATTACK": 145, "DEFENSE": 68, "CRIT_RATE": 12}
      }
    }
    ```

### 1.3 Envanter ve Ekipman
- **GET `/api/v1/characters/:id/inventory`**
  - Yanıt:
    ```json
    {
      "success": true,
      "data": {
        "items": [
          {
            "id": 120402,
            "slot_index": 12,
            "item_definition_id": "sword_01",
            "quantity": 1,
            "durability": 95,
            "custom_modifiers": [{"stat": "ATTACK", "operation": "ADD", "value": 5}]
          }
        ]
      }
    }
    ```
- **POST `/api/v1/inventory/move`**
  - İstek: `{"character_id": 1, "from_slot": 12, "to_slot": 1000}` (Örn: Silah yuvasına ekipman kuşanma)
  - Yanıt: `{"success": true, "data": {"refresh_stats": true}}`

### 1.4 Ekonomi ve Ticaret
- **POST `/api/v1/economy/npc/buy`**
  - İstek: `{"character_id": 1, "npc_id": "merchant_1", "item_definition_id": "red_potion_1", "quantity": 10}`
- **POST `/api/v1/economy/npc/sell`**
  - İstek: `{"character_id": 1, "npc_id": "merchant_1", "inventory_slot": 4}`

---

## 2. WebSocket Protokolü (Gerçek Zamanlı Güncellemeler)

Websocket bağlantıları `wss://<host>/ws?token=<jwt_token>&character_id=<character_id>` adresinde kurulur. Tüm veriler JSON mesaj formatında gönderilir.

### Mesaj Zarfı Yapısı (Message Envelopes)
```json
{
  "type": "MESSAGE_TYPE",
  "payload": { ... }
}
```

### 2.1 İstemciden Gelen Olaylar (Client -> Server)
- **ATTACK**: Hedefe temel saldırı başlatır.
  `{"type": "ATTACK", "payload": {"target_id": 4022, "target_type": "MOB"}}`
- **CAST_SKILL**: Yetenek kullanımını tetikler.
  `{"type": "CAST_SKILL", "payload": {"skill_id": "aura_of_sword", "target_id": 4022, "target_type": "MOB"}}`
- **CHAT**: Genel, lonca veya özel fısıltı sohbeti.
  `{"type": "CHAT", "payload": {"channel": "GUILD", "message": "Gather for Metin stone!"}}`

### 2.2 Sunucudan Giden Olaylar (Server -> Client)
- **COMBAT_DAMAGE**: Hasar miktarlarını, kritikleri, dodge olaylarını ve durum etkilerini bildirir.
  ```json
  {
    "type": "COMBAT_DAMAGE",
    "payload": {
      "attacker_id": 1,
      "defender_id": 4022,
      "damage": 342,
      "is_critical": true,
      "is_miss": false,
      "added_effects": ["burn"]
    }
  }
  ```
- **GOLD_UPDATE** / **EXP_UPDATE**: Pasif ilerleme tikleri veya ganimet bildirimleri.
- **STAT_UPDATE**: Eşyalar değiştiğinde veya buff'ların süresi bittiğinde gönderilir.

---

## 3. Yönetici / Tasarımcı API'si (Admin API)

`Role: ADMIN` JWT doğrulaması ile korunur. Tasarımcı konsolu tarafından gerçek zamanlı oyun parametrelerini güncellemek için kullanılır.

- **POST `/api/v1/admin/items`**: Yeni eşya tanımları ekler veya günceller.
  ```json
  {
    "id": "heaven_tear_shield_0",
    "name": "Cennetin Gözü Kalkan",
    "slot_type": "SHIELD",
    "min_level": 60,
    "stats_modifiers": [
      {"stat": "DEFENSE", "operation": "ADD", "value": 120},
      {"stat": "RESIST_MAGIC", "operation": "ADD", "value": 10}
    ]
  }
  ```
- **POST `/api/v1/admin/skills`**: Yetenek parametrelerini tanımlar veya düzenler.
- **POST `/api/v1/admin/loot`**: Ganimet düşme olasılıklarını yapılandırır.

---

## 4. Go Backend -> Rust zzstat FFI Bağlayıcı Arayüzü

Ağ tabanlı bir gRPC protokolü yerine, statü hesaplamaları süreç-içi (in-process) olarak gerçekleştirilir. Go monoliti, başlangıçta Rust core paylaşımlı kütüphanesini (`libzzstat_ffi.so`) `purego` kullanarak yükler ve doğrudan C FFI bağlayıcıları üzerinden haberleşir.

### Rust Core Tarafından Sunulan FFI Fonksiyonları
Go bağlayıcısı, Rust FFI kütüphanesinden aşağıdaki sembolleri yükler ve sunar:

```go
// Çözümleyici (Resolver) oluşturma ve silme
zzstat_resolver_create() uintptr
zzstat_resolver_free(resolver uintptr)

// Bağlam (Context) oluşturma ve silme
zzstat_context_create() uintptr
zzstat_context_free(ctx uintptr)

// Temel değerleri ve modifikatörleri kaydetme
zzstat_resolver_register_constant_source(resolver uintptr, statID *byte, value float64) int32
zzstat_resolver_register_scaling_transform(resolver uintptr, statID *byte, phase byte, rule byte, dependency *byte, scaleFactor float64) int32
zzstat_resolver_register_multiplicative_transform(resolver uintptr, statID *byte, phase byte, rule byte, value float64) int32

// Hesaplamaları çözümleme (Resolve)
zzstat_resolver_resolve(resolver uintptr, statID *byte, ctx uintptr, outValue *float64) int32
```

Süreç-içi FFI çağrılarının kullanılması, ağ gecikmesini ve serileştirme/seriyi çözme (gRPC/JSON) yükünü tamamen ortadan kaldırarak savaş statüsü güncellemelerini ve hasar hesaplamalarını doğrudan işlemci hızı sınırına taşır.
