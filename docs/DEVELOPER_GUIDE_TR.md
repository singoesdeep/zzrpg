# Geliştirici Kılavuzu: Stat, Eşya ve Savaş Sistemlerini Özelleştirme (TR)

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
Türetilmiş Statüler (Derived Stats - HP, MP, Attack, Defense gibi base statülerden türetilen değerler) Go backend monolith içindeki `backend/internal/statclient/client.go` dosyasında, Rust `zzstat` kütüphanesinin DSL yapısıyla in-process (süreç-içi) olarak tanımlanır ve hesaplanır.

### Formül Değiştirme Adımları:
1. **Dosyayı Açın**: [backend/internal/statclient/client.go](file:///home/singo/github.com/singoesdeep/zzrpg/backend/internal/statclient/client.go) dosyasını açın.
2. **Formül Bloklarını Bulun**: `Calculate` metodunun altındaki derived stat formül tanımlamalarını düzenleyin:
   ```go
   // HP = CON * 15.0
   resolver.RegisterScalingTransform("HP", zzstat.PhaseAdditive, zzstat.RuleAdditive, "CON", 15.0)
   // MP = INT * 10.0
   resolver.RegisterScalingTransform("MP", zzstat.PhaseAdditive, zzstat.RuleAdditive, "INT", 10.0)
   // ATTACK = STR * 2.0 + DEX * 0.5
   resolver.RegisterScalingTransform("ATTACK", zzstat.PhaseAdditive, zzstat.RuleAdditive, "STR", 2.0)
   // ATTACK = STR * 2.0 + DEX * 0.5
   resolver.RegisterScalingTransform("ATTACK", zzstat.PhaseAdditive, zzstat.RuleAdditive, "DEX", 0.5)
   // DEFENSE = CON * 1.0 + STR * 0.2
   resolver.RegisterScalingTransform("DEFENSE", zzstat.PhaseAdditive, zzstat.RuleAdditive, "CON", 1.0)
   resolver.RegisterScalingTransform("DEFENSE", zzstat.PhaseAdditive, zzstat.RuleAdditive, "STR", 0.2)
   ```
3. **Formülü Güncelleyin**: Örneğin, savunmayı güçlendirmek için zırh formülünü `CON * 1.5` ve `STR * 0.4` olarak değiştirebilirsiniz.
4. **Go Testlerini Çalıştırın**:
   ```bash
   cd backend/internal/statclient
   go test -v .
   ```

---

## 3. Savaş İsabet, Kritik ve Varyans Kurallarını Değiştirme
Savaş hesaplamaları (ıskalama şansı, kritik vurma hasarı ve hasar varyansı) Go backend içindeki `backend/internal/statclient/client.go` dosyasında süreç-içi olarak hesaplanır.

### Özelleştirme Adımları:
[backend/internal/statclient/client.go](file:///home/singo/github.com/singoesdeep/zzrpg/backend/internal/statclient/client.go) içindeki `CalculateDamage` fonksiyonunu düzenleyin:

* **İsabet Şansı Capping (Sınırlandırma)**:
  Varsayılan olarak vuruş şansı %70 ile %99 arasında sınırlandırılmıştır. Bunu değiştirmek için:
  ```go
  // Cap hit chance between 70% (0.70) and 99% (0.99)
  hitChance := baseHitChance
  if hitChance < 0.70 {
  	hitChance = 0.70
  } else if hitChance > 0.99 {
  	hitChance = 0.99
  }
  ```
* **Kritik Hasar Çarpanı**:
  Kritik vuruş gerçekleştiğinde hasar varsayılan olarak $1.5$ ile çarpılır. Bunu $2.0$ yapmak için:
  ```go
  isCrit := critRoll < req.Attacker.CritRate
  if isCrit {
  	damage = damage * 2.0 * (1.0 + req.Attacker.CritDamageBonus)
  }
  ```
* **Hasar Varyansı (RNG Salınımı)**:
  Saldırı hasarının stabil olmaması ve $\pm\%10$ salınım yapması için şu kod bulunur. Salınımı değiştirmek için çarpanı düzenleyin:
  ```go
  // RNG Variance (±10%)
  variance := 0.9 + c.rng.Float64()*0.2
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

* **Go Testlerini Çalıştırma (Birim & WebSocket Entegrasyon Testleri)**:
  ```bash
  cd backend
  go test -v ./...
  ```
