# zzrpg Geliştirici Kılavuzu: Stat, Eşya ve Ganimet Sistemlerini Özelleştirme

Bu kılavuz, **zzrpg** projesinde eşya (item) tanımlarını, statü formüllerini, savaş mekaniklerini ve canavar ganimet (loot) oranlarını kod yazarak ya da kod yazmadan nasıl değiştireceğinizi adım adım açıklamaktadır.

---

## 1. Eşya (Item) Tanımlarını ve Statlarını Değiştirme
Eşya tanımları tamamen **veri-odaklı (data-driven)** yapıdadır. Eşyaların kod içinde karşılıkları yoktur; hepsi veritabanında satır olarak tutulur.

### Yöntem A: Scalar API Docs Üzerinden (Canlı Güncelleme)
Sunucu çalışırken yeni bir eşya eklemek veya mevcut bir eşyayı güncellemek için Scalar Docs (`http://localhost:8080/docs`) arayüzünü kullanabilirsiniz:
* **Yeni Eşya Ekleme**: `POST /api/v1/admin/items` endpoint'ine şu JSON gövdesini (payload) gönderin:
  ```json
  {
    "id": "dragon_sword_9",
    "name": "Ejderha Kılıcı +9",
    "item_type": "WEAPON",
    "slot_type": "WEAPON",
    "min_level": 50,
    "class_restriction": "WARRIOR",
    "base_durability": 150,
    "stats": {
      "ATTACK": 150,
      "DEX": 10
    }
  }
  ```
* **Mevcut Eşyayı Güncelleme**: `PUT /api/v1/admin/items/{id}` endpoint'ini kullanarak eşyanın statlarını veya seviye gereksinimlerini canlı olarak güncelleyebilirsiniz.

### Yöntem B: Kalıcı SQL Migration İle (Tüm Ortamlar İçin)
Eğer eklediğiniz eşyanın tüm geliştirme ve canlı ortamlarda otomatik oluşmasını istiyorsanız SQL migration yazmalısınız:
1. `backend/internal/database/migrations/` dizininde yeni bir SQL dosyası açın.
2. Aşağıdaki gibi SQL komutunu ekleyin:
   ```sql
   INSERT INTO item_definitions (id, name, item_type, slot_type, min_level, class_restriction, base_durability, stats)
   VALUES ('phoenix_armor', 'Anka Zırhı', 'ARMOR', 'ARMOR', 40, 'MAGE', 120, '{"DEFENSE": 85, "HP": 200}');
   ```

---

## 2. Statü Hesaplama Formüllerini Değiştirme
Derived Stats (Base statülerden türetilen HP, MP, Attack, Defense gibi son değerler) Rust dilinde geliştirilen `zzstat` gRPC mikroservisi tarafından hesaplanır.

### Formül Değiştirme Adımları:
1. **Dosyayı Açın**: [zzstat/src/main.rs](file:///home/singo/github.com/singoesdeep/zzrpg/zzstat/src/main.rs) dosyasını açın.
2. **Formül Bloklarını Bulun**: `CalculateStats` implementasyonu altındaki derived stat formüllerini düzenleyin:
   ```rust
   // Mevcut Formüller:
   let base_hp = con_val * 15.0; // HP = CON * 15
   let base_mp = int_val * 10.0; // MP = INT * 10
   let base_attack = str_val * 2.0 + dex_val * 0.5; // ATTACK = STR*2 + DEX*0.5
   let base_defense = con_val * 1.0 + str_val * 0.2; // DEFENSE = CON*1 + STR*0.2
   ```
3. **Formülü Güncelleyin**: Örneğin, zırh gücünü artırmak için savunma formülünü `con_val * 1.5 + str_val * 0.4` olarak değiştirebilirsiniz.
4. **Rust Servisini Yeniden Başlatın**:
   ```bash
   cd zzstat
   cargo test # Testlerin çalıştığını doğrulamak için
   cargo run  # Servisi 50051 portunda canlıya almak için
   ```

---

## 3. Savaş İsabet, Kritik ve Varyans Kurallarını Değiştirme
Savaş hesaplamaları (ıskalama şansı, kritik vurma hasarı ve hasar varyansı) da Rust `zzstat` servisindeki `CombatService` tarafından hesaplanır.

### Özelleştirme Adımları:
[zzstat/src/main.rs](file:///home/singo/github.com/singoesdeep/zzrpg/zzstat/src/main.rs) içindeki `calculate_damage` fonksiyonunu düzenleyin:

* **İsabet Şansı Capping (Sınırlandırma)**:
  Varsayılan olarak vuruş şansı %70 ile %99 arasında sınırlandırılmıştır. Bunu esnetmek için:
  ```rust
  let hit_chance = base_hit_chance.clamp(0.50, 0.95); // %50 min, %95 max isabet
  ```
* **Kritik Hasar Çarpanı**:
  Kritik vuruş gerçekleştiğinde hasar varsayılan olarak $1.5$ ile çarpılır. Bunu $2.0$ yapmak için:
  ```rust
  if is_crit {
      damage = damage * 2.0 * (1.0 + attacker.crit_damage_bonus);
  }
  ```
* **Hasar Varyansı (RNG Salınımı)**:
  Saldırı hasarının stabil olmaması ve $\pm\%10$ salınım yapması için şu kod bulunur. Salınımı kapatmak veya değiştirmek için aralığı düzenleyin:
  ```rust
  let variance: f64 = rng.gen_range(0.95..1.05); // Salınımı %5'e düşürür
  ```

---

## 4. Canavar Ganimet Tablolarını (Loot Tables) Değiştirme
Ganimet tabloları veritabanında `loot_tables` tablosunda JSONB formatında saklanır.

### JSONB Ganimet Yapısı:
Her ganimet satırı şu yapıda drop listesi tutar:
```json
[
  {"item_definition_id": "gold", "rate": 5000, "min": 50, "max": 150},
  {"item_definition_id": "dragon_sword_0", "rate": 500, "min": 1, "max": 1}
]
```
* `rate`: 10.000 üzerinden kazanma ihtimali (Örn: 5000 = %50, 500 = %5).
* `min` ve `max`: Düşecek eşya/altın miktar sınırları.

### Güncelleme Yöntemi (API Docs Üzerinden):
`POST /api/v1/admin/loot` endpoint'ine şu payload'u göndererek ganimeti güncelleyebilirsiniz:
```json
{
  "id": "dummy_drops",
  "description": "Eğitim Mankeni Ganimet Tablosu",
  "entries": [
    {
      "item_definition_id": "gold",
      "rate": 10000,
      "min": 100,
      "max": 200
    },
    {
      "item_definition_id": "dragon_sword_0",
      "rate": 1000,
      "min": 1,
      "max": 1
    }
  ]
}
```

---

## 5. Değişikliklerden Sonra Testlerin Çalıştırılması
Yaptığınız herhangi bir değişiklikten sonra sistemin kararlılığını korumak için mutlaka testleri koşturun:

* **Rust Testlerini Çalıştırma**:
  ```bash
  cd zzstat
  cargo test
  ```
* **Go Testlerini Çalıştırma (Birim & WebSocket Entegrasyon Testleri)**:
  ```bash
  cd backend
  go test -v ./...
  ```
  *(Not: Entegrasyon testlerinin çalışabilmesi için yerel bilgisayarda `./scripts/start-infra.sh` ile veritabanı konteynerlerinin açık olması gereklidir.)*
