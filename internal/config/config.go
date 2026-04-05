package config

import (
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	Port        string
	Environment string

	GeminiAPIKey      string
	GoogleCloudKey    string
	EnableCloudVision bool

	DatabasePath string

	RoomDataPath string

	MaxFileSize    int64
	MaxBatchSize   int
	AllowedFormats []string

	EnableLearning    bool
	LearningThreshold float64

	OCRTimeout int
}

func Load() *Config {
	_ = godotenv.Load()

	cfg := &Config{
		Port:              getEnv("PORT", "8080"),
		Environment:       getEnv("ENVIRONMENT", "development"),
		GeminiAPIKey:      getEnv("GEMINI_API_KEY", ""),
		GoogleCloudKey:    getEnv("GOOGLE_CLOUD_VISION_KEY", ""),
		EnableCloudVision: getEnvBool("ENABLE_CLOUD_VISION", false),
		DatabasePath:      getEnv("DB_PATH", "./data/hand_learning.db"),
		RoomDataPath:      getEnv("ROOM_DATA_PATH", "./data/rooms.json"),
		MaxFileSize:       getEnvInt64("MAX_FILE_SIZE", 10*1024*1024),
		MaxBatchSize:      getEnvInt("MAX_BATCH_SIZE", 10),
		AllowedFormats:    []string{".jpg", ".jpeg", ".png", ".webp"},
		EnableLearning:    getEnvBool("ENABLE_LEARNING", true),
		LearningThreshold: getEnvFloat("LEARNING_THRESHOLD", 0.7),
		OCRTimeout:        getEnvInt("OCR_TIMEOUT", 30),
	}

	if cfg.GeminiAPIKey == "" {
		log.Fatal("❌ GEMINI_API_KEY is required")
	}

	if err := os.MkdirAll("./data", 0755); err != nil {
		log.Fatal("❌ Cannot create data directory:", err)
	}

	return cfg
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return strings.TrimSpace(val)
	}
	return defaultVal
}

func getEnvBool(key string, defaultVal bool) bool {
	val := os.Getenv(key)
	if val == "" {
		return defaultVal
	}
	return strings.ToLower(val) == "true" || val == "1"
}

func getEnvInt(key string, defaultVal int) int {
	val := os.Getenv(key)
	if val == "" {
		return defaultVal
	}
	i, err := strconv.Atoi(val)
	if err != nil {
		return defaultVal
	}
	return i
}

func getEnvInt64(key string, defaultVal int64) int64 {
	val := os.Getenv(key)
	if val == "" {
		return defaultVal
	}
	i, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		return defaultVal
	}
	return i
}

func getEnvFloat(key string, defaultVal float64) float64 {
	val := os.Getenv(key)
	if val == "" {
		return defaultVal
	}
	f, err := strconv.ParseFloat(val, 64)
	if err != nil {
		return defaultVal
	}
	return f
}
