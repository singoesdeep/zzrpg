# Mimari Belgesi: zzrpg Backend (TR)

Bu belge, `zzrpg` projesinin üst düzey sistem mimarisini, modüler monolit tasarımını, teknoloji yığınını ve bileşenler arası iletişim kalıplarını özetlemektedir.

## 1. Sistem Mimari Diyagramı

Aşağıda, `zzrpg` sisteminin üst düzey mimari şeması gösterilmektedir. Sistem, Go dilinde geliştirilmiş bir **Modüler Monolit** backend ve statü hesaplamaları için süreç-içi FFI bağlayıcıları vasıtasıyla doğrudan bağlanan, oldukça optimize edilmiş bir **Rust zzstat core kütüphanesi** üzerine kuruludur.

```mermaid
graph TD
    %% Clients
    Browser[Tarayıcı / Next.js İstemci]

    %% Gateway/API Layer
    subgraph Go Backend (Modüler Monolit)
        API[Go API Sunucusu REST/WS]
        
        %% Internal Modules
        subgraph Dahili Modüller
            Auth[Yetkilendirme Modülü]
            Char[Karakter Modülü]
            Inv[Envanter Modülü]
            Equip[Ekipman Modülü]
            Combat[Savaş Modülü]
            Skill[Yetenek Modülü]
            Quest[Görev Modülü]
            Guild[Lonca Modülü]
            Econ[Ekonomi Modülü]
            Loot[Ganimet Modülü]
        end
        
        %% Shared Core Packages
        Database[Veritabanı Paketi - pgx]
        RedisClient[Redis İstemcisi - Oturum/Önbellek/Kilitler]
        WS[WebSocket Yöneticisi]
        StatClient[Statü İstemcisi - FFI]
        EventBus[Süreç-İçi Olay Veriyolu]
    end

    %% External Infrastructure
    DB[(PostgreSQL Veritabanı)]
    Redis[(Redis Önbellek ve Mesaj Aracısı)]
    RustStat[Rust zzstat Core Kütüphanesi]

    %% Connections
    Browser <-->|HTTPS / WSS| API
    
    %% Go Internal relations
    API --> Auth
    API --> Char
    API --> Inv
    API --> Equip
    API --> Combat
    API --> Skill
    API --> Quest
    API --> Guild
    API --> Econ
    API --> Loot
    
    %% Infrastructure access
    Dahili Modüller --> Database
    Dahili Modüller --> RedisClient
    Dahili Modüller --> WS
    Dahili Modüller --> StatClient
    Dahili Modüller --> EventBus
    
    Database <--> DB
    RedisClient <--> Redis
    StatClient <-->|FFI Süreç-İçi Çağrılar| RustStat
```

---

## 2. Go Backend Modül Yapısı

Go backend kod yapısı, temiz mimari (clean architecture), alan yalıtımı (domain isolation) ve modüler monolit prensiplerini takip eder. Her alan; kendi deposu (repository), servisi ve taşıma (transport) işleyicileriyle bağımsız bir yapıdadır.

```
backend/
├── cmd/
│   └── server/
│       └── main.go           # Uygulama giriş noktası, bağımlılık enjeksiyonu ve sunucu başlangıcı
├── internal/
│   ├── auth/                 # Kullanıcı kaydı, yetkilendirme, JWT token işlemleri
│   ├── character/            # Karakter oluşturma, temel bilgiler, seviye/deneyim, çevrimdışı ilerleme
│   ├── inventory/            # Oyuncu envanterleri, eşya depolama, eşya taşıma işlemleri
│   ├── items/                # Eşya tanımları (veri-odaklı), istatistik modifikatörleri
│   ├── equipment/            # Kuşanılmış aktif eşyalar, yuva (slot) doğrulama mantığı
│   ├── combat/               # Dynamic combat loop, calculated via zzstat
│   ├── skills/               # Yetenek şablonları, yetenek seviyeleri, yükseltmeler
│   ├── quests/               # Veri-odaklı görev adımları, ilerleme, ödüller
│   ├── guild/                # Lonca oluşturma, rütbeler, lonca bankası ve statü bonusları
│   ├── economy/              # Altın/para birimleri, pazar işlem günlükleri
│   ├── loot/                 # Olasılık tabanlı ganimet tabloları, canavar drop mekanikleri
│   ├── statclient/           # Süreç-içi FFI istemcisi, Rust zzstat core kütüphanesini yükler ve yürütür
│   ├── database/             # PostgreSQL bağlantı havuzu yapılandırması ve göç (migration) çalıştırıcı
│   ├── events/               # Modülleri gevşek bağlamak (decoupling) için olay yayıncı/abone yapısı
│   └── websocket/            # Bağlantı yöneticisi, hub, okuma/yazma döngüleri ve oyun bildirimleri
├── pkg/
│   ├── config/               # Çevre değişkenleri üzerinden yapılandırma ayrıştırma
│   ├── logger/               # Yapılandırılmış günlük kaydı (slog/zap)
│   └── utils/                # Genel yardımcılar (UUID, şifreleme, vb.)
├── go.mod
├── go.sum
```

### Modül Sınırları ve Bağımlılık Enjeksiyonu (Dependency Injection)
1. **Yalıtım**: Modüller, doğrudan başka bir modülün veritabanı tablolarına erişmemelidir. Diğer modüller tarafından sunulan ortak arayüzleri/servisleri kullanmalıdırlar.
2. **Depo Örüntüsü (Repository Pattern)**: Veritabanı işlemleri, test edilebilir ve taklit edilebilir (mock) olması için depo arayüzleri arkasında soyutlanır.
3. **Alan Olay Veriyolu (Domain Event Bus)**: Sıkı sıkıya bağlı bağımlılıkları önlemek için modüller, uygun olan yerlerde `internal/events` kullanarak asenkron olarak haberleşir (örneğin, bir karakter seviye atladığında, `Character` modülünün içine doğrudan kod yazmadan `Quest` modülü olay veriyolunu dinleyerek görev ilerlemesini günceller).

---

## 3. Teknoloji Yığını

- **Ana Dil**: Hızlı performans, düşük bellek kullanımı, eşzamanlılık (concurrency) araçları ve basit sözdizimi için Go (1.23+).
- **Veritabanı**: Sürücü olarak `pgx` kullanan PostgreSQL (16+). İşlem bütünlüğünü (ACID) kaybetmeden veri-odaklı oyun tasarımları yapabilmek için yoğun JSONB sütun kullanımı.
- **Önbellek ve Mesaj Aracısı**: Oyuncu çevrimiçi durum takibi, oturum depolama, dağıtık kilitler (savaş/ticaret kilitleri) ve WebSocket olay pub/sub işlemleri için Redis (7+).
- **Statü Servisi**: Paylaşımlı kütüphane (`libzzstat_ffi.so`) olarak derlenen ve `purego` FFI bağlayıcıları ile doğrudan Go'ya süreç-içi olarak gömülen Rust tabanlı `zzstat` çekirdek motoru.
- **WebSockets**: Gerçek zamanlı olayları (savaş günlükleri, sohbet, kazanılan eşyalar) tarayıcıya iletmek için standart veya `gorilla/websocket` kütüphanesi.
