package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"hotel-ocr-system/internal/config"
	"hotel-ocr-system/internal/database"
	"hotel-ocr-system/internal/handlers"
	"hotel-ocr-system/internal/ocr"

	"github.com/gin-gonic/gin"
)

func main() {
	cfg := config.Load()

	if cfg.Environment == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	db, err := database.NewSQLiteDB(cfg.DatabasePath)
	if err != nil {
		log.Fatal("❌ Failed to connect database:", err)
	}
	defer db.Close()

	log.Println("🧠 Initializing OCR engine...")
	ocrEngine, err := ocr.NewSmartOCR(cfg, db)
	if err != nil {
		log.Fatal("❌ Failed to initialize OCR:", err)
	}

	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(corsMiddleware())
	r.Use(requestLogger())

	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": "healthy",
			"time":   time.Now().Format(time.RFC3339),
		})
	})

	v1 := r.Group("/api/v1")
	{
		docHandler := handlers.NewDocumentHandler(ocrEngine, db)

		v1.POST("/documents/process", docHandler.ProcessDocument)
		v1.POST("/documents/batch", docHandler.BatchProcess)
		v1.GET("/documents/:id", docHandler.GetDocument)

		v1.POST("/feedback", docHandler.SaveFeedback)
		v1.GET("/feedback/stats", docHandler.GetFeedbackStats)

		v1.GET("/rooms", docHandler.ListRooms)
		v1.GET("/rooms/:number", docHandler.GetRoomInfo)

		v1.GET("/stats/dashboard", docHandler.GetDashboardStats)
	}

	r.Static("/static", "./web/static")

	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: r,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("❌ Server error: %v", err)
		}
	}()

	log.Printf("🚀 Server ready at http://localhost:%s", cfg.Port)
	log.Printf("📚 Environment: %s", cfg.Environment)

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("🛑 Shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Fatal("❌ Server forced to shutdown:", err)
	}

	log.Println("✅ Server exited")
}

func corsMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	}
}

func requestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path

		c.Next()

		latency := time.Since(start)
		status := c.Writer.Status()

		log.Printf("[%s] %s %s %d %v",
			c.Request.Method,
			path,
			c.ClientIP(),
			status,
			latency,
		)
	}
}
