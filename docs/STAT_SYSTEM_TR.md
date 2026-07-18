# Statü Sistemi Tasarımı: Gömülü Rust zzstat FFI Entegrasyonu (TR)

Oyundaki tüm statü hesaplamaları gömülü Rust `zzstat` kütüphanesine devredilmiştir. Go backend kendi başına statü matematiksel işlemleri yapmaz. Aktif modifikatörleri toplayan, bunları süreç-içi (in-process) çözücüye (resolver) kaydeden ve çözülen değerleri önbelleğe alan/kaydeden bir orkestratör görevi görür.

---

## 1. Statü İşlem Hattı (Pipeline)

```
[Karakter Temel Statüleri]  (STR, INT, DEX, CON)
        │
        ▼
[Modifikatörleri Topla]     (Ekipman + Yetenekler + Bufflar + Lonca + Geçici Etkiler)
        │
        ▼
[İstek Payload'unu Oluştur] (Priority ve Source ile StatModifier listesi inşa et)
        │
        ▼
[FFI: Süreç-İçi Çağrılar]   ───(Go İstemcisi)───► [Gömülü Rust Core]
                                                       │
                                                       ├── Statü Tipine Göre Grupla
                                                       ├── Önceliğe Göre Sırala (0 -> Base, 1 -> Add, 2 -> Multiply)
                                                       ├── İstifleme Mantığını Uygula (Aynı Buff SourceID'lerini engelle)
                                                       └── Formülleri Hesapla (Base -> Primary -> Derived -> Final)
                                                       │
[Önbellek ve DB Güncelle]   ◄──(Go Backend)────────────┘
```

---

## 2. Modifikatörler ve İşlemler (Modifiers & Operations)

Her modifikatör şunlara sahiptir:
- **Stat**: Hedef statü (örn: `HP`, `ATTACK`, `DEFENSE`, `CRIT_RATE`, `STR`, `INT`).
- **Operation**: `ADD` veya `MULTIPLY`.
- **Value**: Sayısal değişim miktarı (örn: ADD için `120`, %15 MULTIPLY için `0.15`).
- **Priority**: Hesaplama sırasını belirler.
  - Priority `10`: Temel (Base) değerler.
  - Priority `20`: Ekipman/yeteneklerden gelen düz eklemeler (örn: `+100 ATTACK`).
  - Priority `30`: Yüzdesel çarpanlar (örn: `+20% ATTACK`).
  - Priority `40`: Geç aşama nihai ezme/azaltma değerleri (örn: kalkan duvarı `-50% Hasar Alımı`).
- **Source Tracking**: Modifikatörün nereden geldiğini tanımlar (örn: `Equipment:dragon_sword_0`). Rust motoru tarafından istifleme (stacking) kurallarını çözmek için kullanılır.

---

## 3. İstifleme Kuralları ve Ezmeler (Stacking Rules & Overrides)

Aynı statüyü etkileyen birden fazla etki aktif olduğunda, Rust şu istifleme kurallarını uygular:
- **Benzersiz Kaynak İstifleme**: Benzersiz `source_id` değerlerine sahip ekipman ve yetenekler toplamsal/yüzdesel olarak üst üste eklenir (stack).
- **Buff Ezmeleri**: Aynı `buff_id`'ye sahip buff'lar (örn: farklı oyuncular tarafından atılan iki `aura_of_sword` buff'ı) üst üste eklenmez. Rust sadece en yüksek değere veya en uzun kalan süreye sahip olanı korur.
- **Debuff Sınırı**: `poison` gibi debuff'ların şiddet sınırları olabilir (örn: maksimum hız azaltma %50'yi geçemez).

---

## 4. Statü Formülleri (Veri-Odaklı Türetilmiş Statüler)

Hesaplamaları tamamen ayrık tutmak için, Go istemcisi gömülü zzstat motorunu standart MMORPG türetilmiş formülleriyle yapılandırır.
Girdi olarak Temel Statüler kullanılır: `STR` (Güç), `INT` (Zeka), `DEX` (Çeviklik), `CON` (Anayasa/Canlılık).

1. **HP Hesaplaması**:
   $$\text{Final HP} = (\text{CON} \times 15 + \sum \text{HP\_ADD}) \times (1 + \sum \text{HP\_MULTIPLY})$$

2. **MP Hesaplaması**:
   $$\text{Final MP} = (\text{INT} \times 10 + \sum \text{MP\_ADD}) \times (1 + \sum \text{MP\_MULTIPLY})$$

3. **Saldırı Gücü (Attack) Hesaplaması**:
   $$\text{Base Attack} = (\text{STR} \times 2.0) + (\text{DEX} \times 0.5)$$
   $$\text{Final Attack} = (\text{Base Attack} + \sum \text{ATTACK\_ADD}) \times (1 + \sum \text{ATTACK\_MULTIPLY})$$

4. **Savunma (Defense) Hesaplaması**:
   $$\text{Base Defense} = (\text{CON} \times 1.0) + (\text{STR} \times 0.2)$$
   $$\text{Final Defense} = (\text{Base Defense} + \sum \text{DEFENSE\_ADD}) \times (1 + \sum \text{DEFENSE\_MULTIPLY})$$

---

## 5. Go İstemci Arayüzü (`statclient`)

Go backend, bağımlılıkları azaltmak için aşağıdaki istemci soyutlamasını tanımlar:

```go
package statclient

import "context"

type CharacterState struct {
	CharacterID int32
	BaseStats   map[string]float64
	Equipment   []Modifier
	Skills      []Modifier
	ActiveBuffs []Modifier
}

type Modifier struct {
	Stat      string  // Örn: "HP", "STR", "ATTACK"
	Operation string  // "ADD", "MULTIPLY"
	Value     float64
	Priority  int32
	SourceID  string  // Örn: "item:dragon_sword"
}

type Client interface {
	Calculate(ctx context.Context, state CharacterState) (map[string]float64, error)
}
```

Go backend, oyuncunun güncel önbelleğe alınmış statülerini güncellemek için işlem kapsamları veya olay dinleyicileri (örn: bir eşya kuşanıldığında, seviye atlandığında) içinde bu arayüzü çağırır.
