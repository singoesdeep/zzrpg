# Geliştirme Yol Haritası ve Kilometre Taşları (TR)

Bu belge, **zzrpg** modüler monolit backend projesinin geliştirme adımlarını ve kilometre taşlarını detaylandırmaktadır.

---

## 1. Ay 1: Temeller (Yetkilendirme, Veritabanı ve Temel Statüler)

### Hedef: Temel mimariyi kurmak, veritabanı bağlantı havuzunu entegre etmek, yetkilendirme REST endpoint'lerini hazırlamak, temel karakter sınıflarını oluşturmak ve FFI entegrasyon istemcisi iskeletini tamamlamak.

#### Hafta 1: Ortam ve Yetkilendirme Temelleri
- Go çalışma alanının ve standart dizin yapısının (`cmd/`, `internal/`, `pkg/`) kurulması.
- Çevre değişkenleri kullanılarak dinamik yapılandırma parametrelerinin ayarlanması.
- `pgx/v5` kullanan PostgreSQL bağlantı havuzunun kurulması.
- SQL veritabanı göç (migration) yapısının kurulması.

#### Hafta 2: Kullanıcı Yetkilendirme API'si
- `users` şemasının tasarlanması.
- Kullanıcı Kaydı (Register) ve Giriş (Login) işlemleri için REST API uç noktalarının (endpoints) yazılması.
- JWT yetkilendirme başlıklarının (headers) üretilmesi ve doğrulanması.
- Kullanıcı oluşturma ve kimlik doğrulama işleyicileri için birim testlerin yazılması.

#### Hafta 3: Karakterler ve Veritabanı Statüleri
- `character` modülünün yazılması: karakter oluşturma, karakter seçme, karakterleri listeleme.
- Karakter oluşturulurken `character_stats` temel değerlerinin ilk kurulumunun yapılması.
- Veritabanı durum kalıcılığı için depo (repository) testlerinin yazılması.

#### Hafta 4: Rust zzstat Core FFI Entegrasyonu
- Rust core paylaşımlı kütüphane dışa aktarımlarının yapılması (`libzzstat_ffi.so`).
- `purego` üzerinden paylaşımlı kütüphaneyi yükleyen Go FFI bağlayıcı istemcisinin (`statclient`) yazılması.
- Statü istemcisinin formül kaydı DSL eşlemeleriyle yapılandırılması.
- Süreç-içi türetilmiş statü ve hasar hesaplama fonksiyonlarının yazılması.

---

## 2. Ay 2: Veri-Odaklı Mekanikler (Eşyalar, Envanter ve Görevler)

### Hedef: Yönetici içerik motorunu, envanter sistemini inşa etmek ve ekipman/buff değişikliklerini doğrudan zzstat yeniden hesaplamalarına bağlamak.

#### Hafta 5: Eşya Tanımları ve Yönetici Konsolu
- Dinamik eşya tanım şemalarını destekleyen `items` modülünün yazılması.
- Dinamik olarak eşya oluşturmak ve modifikatörleri tanımlamak için REST Admin API'sinin yazılması: `/api/v1/admin/items`.
- Modifikatör JSONB verisinin doğru biçimlendirildiğinden emin olmak için doğrulama mantığının eklenmesi.

#### Hafta 6: Envanter ve Ekipman Yuvaları
- `inventory` ve `equipment` modüllerinin yazılması.
- Sınıf kurallarına ve minimum seviyeye göre ekipman yuvası atamalarını kısıtlamak için veritabanı tetikleyicilerinin veya servislerinin tasarlanması.
- Envanter düzenlemelerini (taşıma, kuşanma, atma) olay mesajları fırlatmaya bağlama.

#### Hafta 7: Dinamik Statü Hesaplamaları
- `inventory.ItemEquipped` veya `inventory.ItemUnequipped` olaylarını dinleyen olay abonesinin yazılması.
- Olay tetiklendiğinde: karakter temel statülerini, tüm aktif ekipman modifikatörlerini toplama, gömülü Rust `zzstat` motoru üzerinden süreç-içi olarak nihai statüleri hesaplama ve `character_stats.derived_stats` önbelleğini güncelleme.
- Mevcut nihai statüleri sorgulamak için `/api/v1/characters/:id/stats` uç noktasının sunulması.

#### Hafta 8: Görev Motoru ve İlerleme
- Görev adımı modellerinin tasarlanması (`KILL_MOB`, `TALK_NPC`).
- İlerlemeyi takip etmek için `quests` modülünün yazılması.
- Görev tamamlandığında ödüllerin (deneyim, altın, eşya) dağıtılmasının yazılması.

---

## 3. Ay 3: Gerçek Zamanlı Savaş, WebSockets ve Yayın

### Hedef: Hesaplama döngülerini gerçek zamanlı WebSockets'e taşımak, oturum kaydını (session registry) uygulamak, ganimet düşürme mekaniklerini kurmak ve projeyi yayına almak.

#### Hafta 9: WebSockets Ağ Geçidi
- WS sunucusu bağlantı yükseltme (upgrade) işleyicisinin kurulması.
- WebSocket sorgu dizgileri (query strings) içindeki JWT yetkilendirme token'larının doğrulanması.
- Olayları yayınlamak ve oyuncu oturum durumlarını yönetmek için `Hub` bağlantı yöneticisinin inşa edilmesi.
- Çift girişleri (duplicate login) eski oturumu sonlandırarak çözme.

#### Hafta 10: Sockets Tabanlı Savaş Döngüsü
- İstemciden gelen mesajların ayrıştırılması (`COMBAT_ATTACK`).
- Oyuncuların/mob'ların aktif HP ve savaş durumlarını yönetmek için bellek-içi iş parçacığı-güvenli `SessionRegistry` oluşturulması.
- Hedef arama mantığının yazılması (PvP hedefi veya kukla hedef).
- WebSocket hasar olayı bildirimlerinin yayınlanması (`COMBAT_DAMAGE`).

#### Hafta 11: Ganimet Tabloları Motoru
- JSONB düşürme oranlarını destekleyen `loot_tables` şemasının tasarlanması.
- Canavar ölüm olayı tetikleyicilerini ganimet tablosu üretimine bağlama.
- Eşya ve altın ödüllerinin doğrudan oyuncunun envanterine dağıtılması (envanter doluysa yere düşecek şekilde).

#### Hafta 12: Çevrimdışı İlerleme ve Cila
- `last_active_at` zaman damgasının takip edilmesi.
- Giriş yapıldığında çevrimdışı sürenin karşılaştırılması ve STR/INT ölçekli pasif deneyim ile altın kazanımlarının verilmesi.
- Genel sistem denetimi, dokümantasyon yazımı, kod biçimlendirme temizliği ve canlıya alım!
