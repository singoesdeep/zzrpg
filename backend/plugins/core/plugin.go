package core

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/singoesdeep/zzrpg/sdk/engine/admin"
	"github.com/singoesdeep/zzrpg/sdk/engine/bus"
	"github.com/singoesdeep/zzrpg/sdk/engine/eventstream"
	"github.com/singoesdeep/zzrpg/sdk/engine/outbox"
	"github.com/singoesdeep/zzrpg/sdk/engine/plugin"
	"github.com/singoesdeep/zzrpg/sdk/engine/registry"

	"github.com/singoesdeep/zzrpg/backend/platform/database"
	"github.com/singoesdeep/zzrpg/backend/platform/session"
	"github.com/singoesdeep/zzrpg/backend/platform/socket"
	"github.com/singoesdeep/zzrpg/sdk/pkg/cache"
	"github.com/singoesdeep/zzrpg/sdk/pkg/metrics"
)

//go:embed api/*
var apiFS embed.FS

func readyStr(ok bool) string {
	if ok {
		return "up"
	}
	return "down"
}

func nodeID() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "node"
	}
	return fmt.Sprintf("%s-%d", host, os.Getpid())
}

type Plugin struct {
	db              *database.DB
	cache           cache.Cache
	closeCache      func() error
	hub             *socket.Hub
	router          *socket.MessageRouter
	sessionReg      *session.Registry
	outboxRelay     *outbox.Relay
	outboxRetention time.Duration
	eventConsumer   *eventstream.Consumer
	closeStream     func() error
}

func NewPlugin() *Plugin {
	return &Plugin{}
}

func (p *Plugin) AdminInfo() admin.Info {
	return admin.Info{
		Title:       "Core Infrastructure",
		Description: "Engine substrate providing DB pool, Redis cache, WebSockets, outbox relay & event streams",
		Icon:        "fa-server",
		Category:    "Infrastructure",
		Endpoints:   []string{"GET /health", "GET /readyz", "GET /metrics", "GET /docs", "GET /admin", "GET /api/v1/admin/plugins", "WS /ws"},
	}
}

func (p *Plugin) Meta() plugin.Meta { return plugin.Meta{Name: "core"} }

func (p *Plugin) Init(ic plugin.InitContext) error {
	cfg := ic.Config()
	log := ic.Logger()
	reg := ic.Registry()
	mux := ic.Mux()
	ctx := ic.Context()

	db, err := database.NewConnectionPool(cfg, log)
	if err != nil {
		return fmt.Errorf("database connection pool: %w", err)
	}
	p.db = db
	// Apply core schema plus any plugin-owned schema the kernel collected.
	var pluginSets []database.MigrationSet
	if srcs, err := registry.Resolve[[]plugin.MigrationSource](reg, "pluginMigrations"); err == nil {
		for _, s := range srcs {
			pluginSets = append(pluginSets, database.MigrationSet{Module: s.Module, FS: s.FS, Dir: s.Dir})
		}
	}
	if err := db.RunMigrations(ctx, pluginSets...); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}

	var appCache cache.Cache = cache.Noop{}
	if c, closeCache, err := cache.NewRedis(ctx, cfg.RedisURL); err != nil {
		log.Warn("Redis unavailable; caching disabled", "error", err)
	} else {
		log.Info("Connected to Redis for caching", "url", cfg.RedisURL)
		appCache = c
		p.closeCache = closeCache
	}
	p.cache = appCache

	p.hub = socket.NewHub()
	// The hub's logout handler (translating a disconnect into a domain event) is
	// wired by the owning domain plugin, keeping core domain-agnostic.
	p.router = socket.NewMessageRouter()
	// Gate owned WS message types on their plugin's activation state so the
	// Admin Dashboard toggle actually suppresses live traffic.
	if mgr, err := registry.Resolve[*admin.StateManager](reg, "pluginManager"); err == nil {
		p.router.SetGate(mgr.IsActive)
	}
	p.sessionReg = session.NewRegistry()

	p.outboxRelay = outbox.NewRelay(p.db.Store, ic.Bus(), log)
	p.outboxRetention = cfg.OutboxRetention

	// Domain plugins register their own event decoders into this registry during
	// their Init (they run after core), so core stays free of domain imports.
	decoders := p.outboxRelay.Registry()
	if err := registry.Provide(reg, "eventDecoders", decoders); err != nil {
		return err
	}

	p.registerOutboxMetrics(reg)
	p.setupEventStream(ic, decoders)

	if err := registry.Provide(reg, "db", p.db); err != nil {
		return err
	}
	if err := registry.Provide(reg, "session", p.sessionReg); err != nil {
		return err
	}
	if err := registry.Provide(reg, "cache", p.cache); err != nil {
		return err
	}
	if err := registry.Provide(reg, "hub", p.hub); err != nil {
		return err
	}
	if err := registry.Provide(reg, "msgRouter", p.router); err != nil {
		return err
	}

	p.registerHTTPEndpoints(mux, reg, log)
	p.registerWebSocket(ctx, mux, reg, cfg.AllowOrigin)

	return nil
}

func (p *Plugin) registerOutboxMetrics(reg *registry.Registry) {
	if m, err := registry.Resolve[*metrics.Metrics](reg, "metrics"); err == nil {
		m.Registerer().MustRegister(prometheus.NewGaugeFunc(prometheus.GaugeOpts{
			Name: "outbox_undispatched",
			Help: "Number of outbox rows written but not yet dispatched by the relay.",
		}, func() float64 {
			qctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			var n float64
			_ = p.db.Pool.QueryRow(qctx, `SELECT count(*) FROM outbox WHERE published_at IS NULL`).Scan(&n)
			return n
		}))
	}
}

func (p *Plugin) setupEventStream(ic plugin.InitContext, decoders *outbox.Registry) {
	cfg := ic.Config()
	log := ic.Logger()
	ctx := ic.Context()
	nid := nodeID()
	if streamClient, err := eventstream.Dial(ctx, cfg.RedisURL); err != nil {
		log.Warn("Cross-node event streaming disabled; running single-node", "error", err)
	} else if fb, ok := ic.Bus().(*bus.Fanout); ok {
		pub := eventstream.NewPublisher(streamClient, "", nid)
		fb.SetForwarder(func(fctx context.Context, ev bus.Event) {
			if err := pub.Publish(fctx, ev); err != nil {
				log.Error("event fan-out publish failed", "event", ev.Name(), "error", err)
			}
		})
		p.eventConsumer = eventstream.NewConsumer(streamClient, fb.PublishLocal, decoders, "", nid, log)
		p.closeStream = streamClient.Close
		log.Info("Cross-node event streaming enabled", "node", nid)
	} else {
		_ = streamClient.Close()
		log.Warn("Kernel bus is not a Fanout; cross-node streaming disabled")
	}
}

func (p *Plugin) registerHTTPEndpoints(mux plugin.Router, reg *registry.Registry, log *slog.Logger) {
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		if err := p.db.Pool.Ping(ctx); err != nil {
			log.Error("Healthcheck failed: postgres is unreachable", "error", err)
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"status":"DOWN", "database":"UNREACHABLE"}`))
			return
		}

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"UP", "database":"OK"}`))
	})

	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		dbReady := p.db.Pool.Ping(ctx) == nil
		redisReady := p.cache.Ping(ctx) == nil

		status := http.StatusOK
		if !dbReady {
			status = http.StatusServiceUnavailable
		}
		body, _ := json.Marshal(map[string]interface{}{
			"ready":    dbReady,
			"database": readyStr(dbReady),
			"redis":    readyStr(redisReady),
		})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write(body)
	})

	mux.HandleFunc("GET /api/openapi.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		data, err := apiFS.ReadFile("api/openapi.json")
		if err != nil {
			log.Error("Failed to read openapi.json", "error", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(data)
	})
	mux.HandleFunc("GET /docs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		data, err := apiFS.ReadFile("api/docs.html")
		if err != nil {
			log.Error("Failed to read docs.html", "error", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(data)
	})

	mux.HandleFunc("GET /admin", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		data, err := apiFS.ReadFile("api/admin.html")
		if err != nil {
			log.Error("Failed to read admin.html", "error", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = w.Write(data)
	})

	mux.HandleFunc("GET /api/v1/admin/plugins", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		mgr, err := registry.Resolve[*admin.StateManager](reg, "pluginManager")
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to resolve plugin manager"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"plugins": mgr.List(),
		})
	})

	mux.HandleFunc("POST /api/v1/admin/plugins/{name}/toggle", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		name := r.PathValue("name")
		mgr, err := registry.Resolve[*admin.StateManager](reg, "pluginManager")
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "failed to resolve plugin manager"})
			return
		}
		newStatus, terr := mgr.Toggle(name)
		if terr != nil {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": terr.Error()})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true,
			"name":    name,
			"status":  newStatus,
		})
	})
}

func (p *Plugin) registerWebSocket(ctx context.Context, mux plugin.Router, reg *registry.Registry, allowOrigin func(origin string) bool) {
	p.router.Handle("CHAT", func(client *socket.Client, msg socket.WSMessage) {
		var payload socket.ChatPayload
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			return
		}
		broadMsg, _ := json.Marshal(map[string]interface{}{
			"type": "CHAT",
			"payload": map[string]interface{}{
				"username": client.Username,
				"message":  payload.Message,
			},
		})
		p.hub.Broadcast <- broadMsg
	})

	// Core only cleans up the transport-level session on disconnect; domain
	// side effects (e.g. last-active, logout events) are wired by domain plugins
	// via the hub's logout handler.
	disconnect := func(client *socket.Client) {
		if client.CharacterID > 0 {
			p.sessionReg.EndSession(client.CharacterID)
		}
	}
	// The WS authenticator is provided by an auth plugin (if any) under
	// "wsAuthenticator"; resolved per-connection since that plugin inits after
	// core registers this route. No authenticator => connections are rejected.
	authenticate := func(token string) (int64, string, bool) {
		a, err := registry.Resolve[socket.Authenticator](reg, "wsAuthenticator")
		if err != nil {
			return 0, "", false
		}
		return a(token)
	}
	mux.HandleFunc("/ws", socket.ServeWS(ctx, p.hub, authenticate, allowOrigin, p.router.Dispatch, disconnect))
}

func (p *Plugin) Start(rc plugin.RunContext) error {
	go p.hub.Run()
	go p.outboxRelay.Run(rc.Context(), time.Second)
	go p.outboxRelay.RunPruner(rc.Context(), 10*time.Minute, p.outboxRetention)
	if p.eventConsumer != nil {
		go p.eventConsumer.Run(rc.Context())
	}
	return nil
}

func (p *Plugin) Stop(ctx context.Context) error {
	if p.closeStream != nil {
		_ = p.closeStream()
	}
	if p.closeCache != nil {
		_ = p.closeCache()
	}
	if p.db != nil {
		p.db.Close()
	}
	return nil
}
