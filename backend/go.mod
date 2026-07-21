module github.com/singoesdeep/zzrpg/backend

go 1.26.5

require (
	github.com/golang-jwt/jwt/v5 v5.3.1
	github.com/gorilla/websocket v1.5.3
	github.com/jackc/pgx/v5 v5.10.0
	github.com/joho/godotenv v1.5.1 // indirect
	github.com/prometheus/client_golang v1.24.0
	github.com/redis/go-redis/v9 v9.21.0 // indirect
	github.com/singoesdeep/zzstat/bindings/go v0.0.0-20260719175047-a9e1e78827b5
	golang.org/x/crypto v0.54.0
)

require (
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/ebitengine/purego v0.7.1 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.70.0 // indirect
	github.com/prometheus/procfs v0.21.1 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.40.0 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)

require github.com/singoesdeep/zzrpg/sdk v0.0.0

replace github.com/singoesdeep/zzrpg/sdk => ../sdk

require github.com/singoesdeep/zzrpg/gamekit v0.0.0

replace github.com/singoesdeep/zzrpg/gamekit => ../gamekit
