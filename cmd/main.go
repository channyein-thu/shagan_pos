package main

import (
	"log"
	"log/slog"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"shagan_pos/cmd/api"
	"shagan_pos/internal/healthcheck"
	"shagan_pos/internal/middleware"
)

func main() {
	if err := godotenv.Load(); err != nil {
		slog.Warn("no .env file found, relying on process environment")
	}

	db, err := connectDB()
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}

	r := gin.Default()
	r.Use(middleware.CORS())

	r.GET("/healthz", healthcheck.Handler(db))

	v1 := r.Group("/api/v1")
	v1.Use(middleware.Auth())

	registerRoutes(v1, db)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8000"
	}

	if err := r.Run(":" + port); err != nil {
		log.Fatalf("server stopped: %v", err)
	}
}

// registerRoutes wires each domain's repository -> service -> API handler and mounts its routes.
// TODO: as each domain grows, this is the place new sub-groups (e.g. per-branch scoping) get added.
func registerRoutes(v1 *gin.RouterGroup, db *gorm.DB) {
	api.NewIdentityAPI(db).RegisterRoutes(v1)
	api.NewCustomerAPI(db).RegisterRoutes(v1)
	api.NewPlatformAPI(db).RegisterRoutes(v1)
	api.NewCatalogAPI(db).RegisterRoutes(v1)
	api.NewProcurementAPI(db).RegisterRoutes(v1)
	api.NewInventoryAPI(db).RegisterRoutes(v1)
	api.NewSalesAPI(db).RegisterRoutes(v1)
	api.NewReturnsAPI(db).RegisterRoutes(v1)
	api.NewShiftAPI(db).RegisterRoutes(v1)
	api.NewSyncAPI(db).RegisterRoutes(v1)
	api.NewAuditAPI(db).RegisterRoutes(v1)
}

func connectDB() (*gorm.DB, error) {
	dsn := os.Getenv("DATABASE_URL")
	return gorm.Open(postgres.Open(dsn), &gorm.Config{})
}
