package main

import (
	"log"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/meowexort/voice-api/internal/config"
	"github.com/meowexort/voice-api/internal/database"
	"github.com/meowexort/voice-api/internal/handler"
	"github.com/meowexort/voice-api/internal/storage"
)

func main() {
	cfg := config.Load()

	db, err := database.Connect(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	defer db.Close()

	if err := database.RunMigrations(cfg.DatabaseURL); err != nil {
		log.Fatalf("failed to run migrations: %v", err)
	}

	rdb := storage.NewRedisClient(cfg.RedisURL)
	defer rdb.Close()

	minioClient, err := storage.NewMinioClient(cfg.MinioEndpoint, cfg.MinioAccessKey, cfg.MinioSecretKey)
	if err != nil {
		log.Fatalf("failed to connect to minio: %v", err)
	}

	r := gin.Default()

	h := handler.New(db, rdb, minioClient, cfg.JWTSecret)
	h.RegisterRoutes(r)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("starting voice-api on :%s", port)
	if err := r.Run(":" + port); err != nil {
		log.Fatalf("failed to start server: %v", err)
	}
}
