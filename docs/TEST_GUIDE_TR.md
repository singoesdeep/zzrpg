# Test Kılavuzu: zzrpg Month 1 Kurulumu (TR)

Altyapıyı çalıştırmak, servisleri başlatmak ve API'leri `curl` kullanarak test etmek için aşağıdaki adımları takip edin.

---

## Adım 1: Podman Altyapısını Başlatın

PostgreSQL ve Redis veritabanlarını başlatmak için proje kök dizinindeki yardımcı betiği çalıştırın:
```bash
./scripts/start-infra.sh
```

---

## Adım 2: Rust zzstat Paylaşımlı Kütüphanesini Derleyin

Bir terminal açın ve Rust core FFI bağlayıcı dinamik kütüphanesini derleyin:
```bash
cargo build --release -p zzstat-ffi
```
Kütüphanenin `zzstat/target/release/libzzstat_ffi.so` yolunda derlendiğinden emin olun. Go backend istemcisi çalışma zamanında bu kütüphaneyi dinamik olarak yükleyecektir.

---

## Adım 3: Go Backend Sunucusunu Başlatın

İkinci bir terminal açın, `backend/` dizinine gidin ve sunucuyu çalıştırın:
```bash
cd backend
go run ./cmd/server
```
Şu çıktıyı görmelisiniz:
```
Starting zzrpg backend...
Connecting to PostgreSQL...
Successfully connected to PostgreSQL
Running database migrations...
All database migrations completed successfully
HTTP server listening on :8080
```
Bu terminali açık tutun.

---

## Adım 4: curl ile Test İstekleri Gönderin

Test komutlarını çalıştırmak için üçüncü bir terminal açın:

### 1. API ve Veritabanı Sağlık Durumunu Kontrol Edin
```bash
curl -i http://localhost:8080/health
```
**Beklenen Yanıt (HTTP 200):**
```json
{"status":"UP", "database":"OK"}
```

### 2. Yeni Kullanıcı Kaydı Yapın
```bash
curl -i -X POST \
  -H "Content-Type: application/json" \
  -d '{"username":"singo","email":"singo@test.com","password":"password123"}' \
  http://localhost:8080/api/v1/auth/register
```
**Beklenen Yanıt (HTTP 201):**
```json
{"success":true,"data":{"user_id":1,"username":"singo","email":"singo@test.com"}}
```

### 3. Giriş Yapın ve JWT Token Alın
```bash
curl -i -X POST \
  -H "Content-Type: application/json" \
  -d '{"username":"singo","password":"password123"}' \
  http://localhost:8080/api/v1/auth/login
```
**Beklenen Yanıt (HTTP 200):**
```json
{
  "success": true,
  "data": {
    "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
    "expires_in": 86400
  }
}
```
*Sonraki adımlarda kullanmak üzere yanıttaki `token` değerini kopyalayın.*

---

## Adım 5: Yetkilendirilmiş Endpoint'leri Test Edin

Token'ı terminalinizde geçici bir çevre değişkeni olarak tanımlayın:
```bash
export TOKEN="kopyaladığınız_token_değeri"
```

### 1. Kullanıcı Profil Bilgilerini Doğrulayın (Me endpoint)
```bash
curl -i -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/v1/auth/me
```
**Beklenen Yanıt (HTTP 200):**
```json
{"success":true,"data":{"user_id":1,"username":"singo"}}
```

### 2. Bir Karakter Oluşturun (WARRIOR sınıfı)
```bash
curl -i -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"WarriorGod","class_name":"WARRIOR"}' \
  http://localhost:8080/api/v1/characters
```
**Beklenen Yanıt (HTTP 201):**
```json
{
  "success": true,
  "data": {
    "id": 1,
    "user_id": 1,
    "name": "WarriorGod",
    "class_name": "WARRIOR",
    "level": 1,
    "experience": 0,
    "gold": 0,
    "last_active_at": "2026-07-18T...",
    "created_at": "2026-07-18T...",
    "updated_at": "2026-07-18T...",
    "stats": {
      "character_id": 1,
      "base_stats": {"CON": 15, "DEX": 10, "INT": 5, "STR": 15},
      "derived_stats": {"HP": 225, "MP": 50, "ATTACK": 30, "DEFENSE": 15, "CRIT_RATE": 5},
      "updated_at": "2026-07-18T..."
    }
  }
}
```

### 3. Tüm Karakterlerinizi Listeleyin
```bash
curl -i -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/v1/characters
```
**Beklenen Yanıt (HTTP 200):**
```json
{"success":true,"data":[{"id":1,"user_id":1,"name":"WarriorGod","class_name":"WARRIOR","level":1,"experience":0,"gold":0,"last_active_at":"...","created_at":"...","updated_at":"..."}]}
```

### 4. Karakter #1'in Detaylarını ve Statülerini Getirin
```bash
curl -i -H "Authorization: Bearer $TOKEN" http://localhost:8080/api/v1/characters/1
```
**Beklenen Yanıt (HTTP 200):**
Karakter özelliklerini ve hesaplanmış başlangıç statülerini gösteren benzer detaylı JSON yanıtı.
