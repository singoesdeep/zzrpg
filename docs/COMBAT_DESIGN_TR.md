# Savaş Sistemi Tasarımı: Veri-Odaklı Mekanikler (TR)

Savaş modülü, gerçek zamanlı savaş hesaplamalarını yönetir. PostgreSQL/Redis'ten oyuncu statülerini çekerek (bu statüler `zzstat` motoru kullanılarak Go-Rust FFI aracılığıyla süreç-içi olarak hesaplanır), savaş sistemi hasar, kritik vuruş, ıskalama, bloklama ve durum etkilerini uygular.

---

## 1. Savaş Yürütme İşlem Hattı (Combat Execution Pipeline)

```
          [İstemci Saldırı Tetikleyicisi] (WebSocket)
                     │
                     ▼
          [Saldırgan ve Hedefi Yükle]
        (Önbellekteki Türetilmiş Statüler)
                     │
                     ▼
         [İsabet / Iskalama Hesapla]
         (DEX ve Dodge oranı kontrolü)
        /                          \
   (İsabet)                       (Iskalama) ──► [Iskalama Olayını Yayınla]
      │
      ▼
[Ham Hasarı Hesapla] (Taban Saldırı - Savunma)
      │
      ▼
[Yetenek Çarpanlarını ve KRİTİK'i Uygula]
      │
      ▼
[Nihai Çarpanları ve Varyansı Uygula] (±10% RNG)
      │
      ▼
  [Durum Etkilerini Belirle] (Zehir, Yanma, Sersemletme)
      │
      ▼
   [HP Azaltmalarını Uygula ve Kaydet]
      │
      ├────────────────────────┐
      ▼                        ▼
[Hasarı Yayınla]        [Hedef Öldü mü?] ──► [Ganimet, Tecrübe, Görevleri Tetikle]
```

---

## 2. İsabet ve Iskalama Mekanikleri

Bir saldırının başarıyla isabet edip etmediğini belirlemek için:

$$\text{Dodge Oranı (Savuşturma)} = \frac{\text{Savunan DEX} \times 0.2 + \text{Savunan Dodge Modifikatörleri}}{\text{Saldırgan Seviyesi} \times 1.5}$$
$$\text{Taban İsabet Şansı} = (100\% - \text{Dodge Oranı}) + \text{Saldırgan İsabet Modifikatörleri}$$

- **Sınırlandırma Kuralları**: Vuruş şansı %70 (minimum garanti vuruş) ile %99 (her zaman küçük de olsa bir ıskalama şansı) arasında sınırlandırılmıştır.
- Saldırı ıskalarsa savaş adımları durdurulur ve WebSocket üzerinden `is_miss: true` ve `damage: 0` içeren bir `COMBAT_DAMAGE` paketi gönderilir.

---

## 3. Hasar Hesaplama Formülleri

### 3.1 Normal Fiziksel Saldırı
$$\text{Hasar}_{\text{normal}} = \max(1, \text{Saldırgan Saldırı} - \text{Savunan Savunma})$$

### 3.2 Yetenek Saldırısı (Örn: Alev Topu)
Yetenek çarpanları, oyuncunun güncel yetenek seviyesi kullanılarak veritabanındaki `skill_definitions` tablosundan çekilir.
$$\text{Taban Hasar} = \text{Saldırgan Saldırı} \times \text{SkillMultiplier}_{\text{seviye}} + \text{SkillFlatDamage}_{\text{seviye}}$$
$$\text{Hasar}_{\text{yetenek}} = \max(1, \text{Taban Hasar} - \text{Savunan Savunma})$$

### 3.3 Kritik Vuruş ve Hasar Varyansı
- **Kritik Vuruş**: Eğer rastgele bir roll saldırganın `CRIT_RATE` değerinin altındaysa hasar katlanır:
  $$\text{Hasar}_{\text{kritik}} = \text{Hasar} \times 1.5 \times (1 + \text{CritDamageBonus})$$
- **RNG Hasar Varyansı**: Hasar değerlerinin tekdüze olmaması için nihai hasara $\pm\%10$ salınım eklenir:
  $$\text{Nihai Hasar} = \text{Hasar} \times \text{UniformRandom}(0.9, 1.1)$$

---

## 4. Durum Etkileri ve Debuff'lar

Yetenekler, veritabanındaki `skill_definitions` metadata alanında tanımlanan ikincil durum etkilerini (debuff) tetikleyebilir:
```json
{
  "status_effects": [
    {
      "buff_definition_id": "poison",
      "chance": 0.20,
      "duration_ms": 10000
    }
  ]
}
```
Eğer durum etkisi tetiklenirse:
1. Go backend, aktif debuff'ı `active_buffs` tablosuna ekler.
2. Bu durum savunanın statülerini geçersiz (invalidate) kılar.
3. Savunanın statüleri süreç-içi çalışan `zzstat` motoru tarafından yeniden hesaplanır (örn: hızı %30 azalır veya her saniye can kaybeder).
4. Harita kanalına statü güncellemesi yayınlanır.

---

## 5. Ölüm ve Ganimet Dağıtımı

Hedefin canı $\le 0$ değerine düşerse:
1. Savaş modülü karakteri/canavarı ölü olarak işaretler.
2. `EventBus` üzerinden asenkron bir olay fırlatılır (`events.CharacterKilled` veya `events.MonsterKilled`).
3. **Loot Modülü** bu olayı yakalar, canavarın `loot_table` tanımına göre drop roll işlemi yapar ve ganimetleri dağıtır.
4. **Görev Modülü** bu olayı yakalar, aktif görevlerdeki canavar öldürme sayılarını artırır ve ilerleme bildirimleri gönderir.
5. **Karakter Modülü** saldırgana tecrübe puanı (EXP) ve altın verir.
