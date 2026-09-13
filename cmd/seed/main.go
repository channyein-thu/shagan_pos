package main

import (
	"log"
	"os"

	"github.com/joho/godotenv"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"shagan_pos/internal/migrate"
	"shagan_pos/internal/seed"
)

func main() {
	_ = godotenv.Load()

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL is required")
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}

	if err := migrate.Run(db); err != nil {
		log.Fatalf("failed to migrate database: %v", err)
	}

	if err := seed.Run(db); err != nil {
		log.Fatalf("seed failed: %v", err)
	}

	log.Println("seed completed")
}
