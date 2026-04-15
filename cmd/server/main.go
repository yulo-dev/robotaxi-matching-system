package main

import (
	"context"
	"log"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
	"github.com/rs/cors"
	"github.com/robotaxi/internal/api"
	"github.com/robotaxi/internal/db"
	"github.com/robotaxi/internal/location"
	"github.com/robotaxi/internal/matching"
	"github.com/robotaxi/internal/middleware"
	"github.com/robotaxi/internal/ws"
)

func main() {
	ctx := context.Background()

	pgURL := envOr("DATABASE_URL", "postgres://robotaxi:robotaxi@localhost:5432/robotaxi?sslmode=disable")
	redisAddr := envOr("REDIS_URL", "localhost:6379")
	port := envOr("PORT", "8080")

	// PostgreSQL
	store, err := db.NewStore(pgURL)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	defer store.Close()
	if err := store.RunMigrations(ctx); err != nil {
		log.Fatalf("migrations: %v", err)
	}
	log.Println("✓ PostgreSQL connected, migrations applied")

	// Redis
	rdb := redis.NewClient(&redis.Options{Addr: redisAddr})
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Fatalf("redis: %v", err)
	}
	log.Println("✓ Redis connected")

	// Services
	locSvc := location.NewService(rdb)
	hub := ws.NewHub()
	matchSvc := matching.NewService(rdb, locSvc, store)
	matchSvc.SetHub(hub)

	// Log dispatch events
	go func() {
		for evt := range matchSvc.DispatchCh {
			log.Printf("⚡ DISPATCH → ride=%s av=%s pickup=(%.4f,%.4f)",
				evt.RideID, evt.AVID, evt.Pickup.Lat, evt.Pickup.Lng)
		}
	}()

	// Gin
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery(), gin.Logger(), middleware.PrometheusMiddleware())

	handler := api.NewHandler(store, locSvc, matchSvc)
	handler.RegisterRoutes(r)

	r.GET("/ws", hub.HandleWS)
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "ws_connections": hub.ClientCount()})
	})

	r.StaticFS("/dashboard", http.Dir("./frontend"))
	r.GET("/", func(c *gin.Context) {
		c.Redirect(http.StatusMovedPermanently, "/dashboard/")
	})

	corsHandler := cors.New(cors.Options{
		AllowedOrigins: []string{"*"}, AllowedMethods: []string{"GET", "POST", "PUT", "DELETE"},
		AllowedHeaders: []string{"*"}, AllowCredentials: true,
	})

	log.Printf("🚕 Robotaxi server on :%s", port)
	log.Printf("   Dashboard:  http://localhost:%s/dashboard/", port)
	log.Printf("   Metrics:    http://localhost:%s/metrics", port)
	log.Printf("   WebSocket:  ws://localhost:%s/ws", port)

	if err := (&http.Server{Addr: ":" + port, Handler: corsHandler.Handler(r)}).ListenAndServe(); err != nil {
		log.Fatalf("server: %v", err)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
