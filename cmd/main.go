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
	"shagan_pos/internal/identity"
	"shagan_pos/internal/middleware"
	"shagan_pos/internal/migrate"
	"shagan_pos/internal/storage"
)

func main() {
	if err := godotenv.Load(); err != nil {
		slog.Warn("no .env file found, relying on process environment")
	}

	db, err := connectDB()
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}

	if err := migrate.Run(db); err != nil {
		log.Fatalf("failed to migrate database: %v", err)
	}

	store, err := connectStorage()
	if err != nil {
		log.Fatalf("failed to connect to object storage: %v", err)
	}

	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		log.Fatal("JWT_SECRET is required")
	}

	r := gin.Default()
	if err := r.SetTrustedProxies(nil); err != nil {
		log.Fatalf("failed to configure trusted proxies: %v", err)
	}
	r.Use(middleware.CORS())

	r.GET("/healthz", healthcheck.Handler(db))
	r.GET("/health", healthcheck.Handler(db))

	v1 := r.Group("/api/v1")
	v1.Use(middleware.Auth())

	registerRoutes(v1, db, store, jwtSecret)

	internalGroup := r.Group("/internal")
	internalGroup.Use(middleware.InternalAuth(os.Getenv("INTERNAL_API_KEY")))
	registerInternalRoutes(internalGroup, db, jwtSecret)

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
func registerRoutes(v1 *gin.RouterGroup, db *gorm.DB, store storage.Storage, jwtSecret string) {
	api.NewIdentityAPI(db, []byte(jwtSecret), identity.DefaultAccessTokenTTL, identity.DefaultRefreshTokenTTL).RegisterRoutes(v1)
	api.NewCustomerAPI(db).RegisterRoutes(v1)
	api.NewPlatformAPI(db, store).RegisterRoutes(v1)
	api.NewCatalogAPI(db).RegisterRoutes(v1)
	api.NewProcurementAPI(db).RegisterRoutes(v1)
	api.NewInventoryAPI(db).RegisterRoutes(v1)
	api.NewSalesAPI(db).RegisterRoutes(v1)
	api.NewReturnsAPI(db).RegisterRoutes(v1)
	api.NewShiftAPI(db).RegisterRoutes(v1)
	api.NewSyncAPI(db).RegisterRoutes(v1)
	api.NewAuditAPI(db).RegisterRoutes(v1)
	api.NewReportsAPI(db).RegisterRoutes(v1)
}

// registerInternalRoutes mounts routes meant only for Shagan's own internal
// tooling (e.g. provisioning a new customer's account) - see middleware.InternalAuth.
func registerInternalRoutes(rg *gin.RouterGroup, db *gorm.DB, jwtSecret string) {
	api.NewIdentityAPI(db, []byte(jwtSecret), identity.DefaultAccessTokenTTL, identity.DefaultRefreshTokenTTL).RegisterInternalRoutes(rg)
}

func connectDB() (*gorm.DB, error) {
	dsn := os.Getenv("DATABASE_URL")
	return gorm.Open(postgres.Open(dsn), &gorm.Config{})
}

func connectStorage() (storage.Storage, error) {
	return storage.NewMinIOStorage(
		os.Getenv("STORAGE_ENDPOINT"),
		os.Getenv("STORAGE_ACCESS_KEY"),
		os.Getenv("STORAGE_SECRET_KEY"),
		os.Getenv("STORAGE_BUCKET"),
		os.Getenv("STORAGE_USE_SSL") == "true",
	)
}
