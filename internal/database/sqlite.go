package database

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"hotel-ocr-system/internal/models"

	_ "github.com/glebarez/sqlite"
	"github.com/google/uuid"
)

type SQLiteDB struct {
	db *sql.DB
}

type HandwritingSample struct {
	ID            int
	ImageHash     string
	ExtractionID  string
	RawText       string
	CorrectedText string
	FieldType     string
	Confidence    float64
	UserFeedback  *bool
	UserNotes     string
	ContextJSON   string
	CreatedAt     time.Time
}

func NewSQLiteDB(dbPath string) (*SQLiteDB, error) {
	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("cannot open database: %w", err)
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("cannot ping database: %w", err)
	}

	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)

	if err := createTables(db); err != nil {
		return nil, fmt.Errorf("cannot create tables: %w", err)
	}

	log.Println("✅ Database connected:", dbPath)
	return &SQLiteDB{db: db}, nil
}

func createTables(db *sql.DB) error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS extractions (
			id TEXT PRIMARY KEY,
			image_hash TEXT UNIQUE,
			file_name TEXT,
			json_data TEXT,
			overall_confidence REAL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,

		`CREATE TABLE IF NOT EXISTS handwriting_samples (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			image_hash TEXT,
			extraction_id TEXT,
			raw_text TEXT,
			corrected_text TEXT,
			field_type TEXT,
			confidence_score REAL,
			user_feedback INTEGER,
			user_notes TEXT,
			context_json TEXT,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (extraction_id) REFERENCES extractions(id)
		)`,

		`CREATE TABLE IF NOT EXISTS learned_patterns (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			pattern_key TEXT UNIQUE,
			character_pair TEXT,
			similarity_score REAL,
			occurrence_count INTEGER DEFAULT 1,
			success_count INTEGER DEFAULT 0,
			last_seen TIMESTAMP,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,

		`CREATE TABLE IF NOT EXISTS vocabulary (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			word TEXT UNIQUE,
			word_type TEXT,
			frequency INTEGER DEFAULT 1,
			common_misspellings TEXT,
			last_used TIMESTAMP,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,

		`CREATE TABLE IF NOT EXISTS daily_stats (
			date TEXT PRIMARY KEY,
			total_processed INTEGER DEFAULT 0,
			success_count INTEGER DEFAULT 0,
			average_confidence REAL DEFAULT 0,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,

		`CREATE INDEX IF NOT EXISTS idx_samples_raw ON handwriting_samples(raw_text)`,
		`CREATE INDEX IF NOT EXISTS idx_samples_type ON handwriting_samples(field_type)`,
		`CREATE INDEX IF NOT EXISTS idx_samples_extraction ON handwriting_samples(extraction_id)`,
		`CREATE INDEX IF NOT EXISTS idx_patterns_key ON learned_patterns(pattern_key)`,
		`CREATE INDEX IF NOT EXISTS idx_vocab_word ON vocabulary(word)`,
		`CREATE INDEX IF NOT EXISTS idx_vocab_type ON vocabulary(word_type)`,
	}

	for _, query := range queries {
		if _, err := db.Exec(query); err != nil {
			return fmt.Errorf("query failed: %s - %w", query[:50], err)
		}
	}

	return nil
}

func (s *SQLiteDB) SaveExtraction(doc *models.ExtractedDocument) error {
	jsonData, err := json.Marshal(doc)
	if err != nil {
		return err
	}

	if doc.ID == "" {
		doc.ID = uuid.New().String()
	}

	_, err = s.db.Exec(`
		INSERT INTO extractions (id, image_hash, file_name, json_data, overall_confidence, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(image_hash) DO UPDATE SET
			json_data = excluded.json_data,
			overall_confidence = excluded.overall_confidence,
			updated_at = excluded.updated_at
	`, doc.ID, doc.ImageHash, doc.FileName, string(jsonData), doc.Confidence, time.Now())

	return err
}

func (s *SQLiteDB) GetExtraction(id string) (*models.ExtractedDocument, error) {
	var jsonData string
	err := s.db.QueryRow("SELECT json_data FROM extractions WHERE id = ?", id).Scan(&jsonData)
	if err != nil {
		return nil, err
	}

	var doc models.ExtractedDocument
	if err := json.Unmarshal([]byte(jsonData), &doc); err != nil {
		return nil, err
	}

	return &doc, nil
}

func (s *SQLiteDB) FindSimilarNames(rawText string, limit int) []HandwritingSample {
	rows, err := s.db.Query(`
		SELECT raw_text, corrected_text, confidence_score, user_feedback
		FROM handwriting_samples
		WHERE field_type = 'name' 
		  AND user_feedback = 1
		  AND (raw_text LIKE ? OR corrected_text LIKE ?)
		ORDER BY 
			CASE 
				WHEN raw_text = ? THEN 1
				WHEN raw_text LIKE ? THEN 2
				ELSE 3
			END,
			created_at DESC
		LIMIT ?
	`, "%"+rawText+"%", "%"+rawText+"%", rawText, rawText+"%", limit)

	if err != nil {
		return nil
	}
	defer rows.Close()

	var results []HandwritingSample
	for rows.Next() {
		var s HandwritingSample
		var feedback sql.NullInt64
		rows.Scan(&s.RawText, &s.CorrectedText, &s.Confidence, &feedback)
		if feedback.Valid {
			correct := feedback.Int64 == 1
			s.UserFeedback = &correct
		}
		results = append(results, s)
	}

	return results
}

func (s *SQLiteDB) GetRecentCorrections(limit int) []HandwritingSample {
	rows, err := s.db.Query(`
		SELECT raw_text, corrected_text, field_type
		FROM handwriting_samples
		WHERE user_feedback = 0 OR corrected_text != raw_text
		ORDER BY created_at DESC
		LIMIT ?
	`, limit)

	if err != nil {
		return nil
	}
	defer rows.Close()

	var results []HandwritingSample
	for rows.Next() {
		var s HandwritingSample
		rows.Scan(&s.RawText, &s.CorrectedText, &s.FieldType)
		results = append(results, s)
	}

	return results
}

func (s *SQLiteDB) SaveFeedback(sample *HandwritingSample) error {
	_, err := s.db.Exec(`
		INSERT INTO handwriting_samples 
		(image_hash, extraction_id, raw_text, corrected_text, field_type, 
		 confidence_score, user_feedback, user_notes, context_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, sample.ImageHash, sample.ExtractionID, sample.RawText, sample.CorrectedText,
		sample.FieldType, sample.Confidence, sample.UserFeedback,
		sample.UserNotes, sample.ContextJSON)

	if err != nil {
		return fmt.Errorf("failed to save feedback: %w", err)
	}

	s.updateStats(sample.FieldType, sample.UserFeedback != nil && *sample.UserFeedback)

	return nil
}

func (s *SQLiteDB) updateStats(fieldType string, isCorrect bool) {
}

func (s *SQLiteDB) GetFeedbackStats() (*models.FeedbackStats, error) {
	var stats models.FeedbackStats

	row := s.db.QueryRow("SELECT COUNT(*) FROM handwriting_samples")
	row.Scan(&stats.TotalSamples)

	var correct, incorrect int64
	s.db.QueryRow("SELECT COUNT(*) FROM handwriting_samples WHERE user_feedback = 1").Scan(&correct)
	s.db.QueryRow("SELECT COUNT(*) FROM handwriting_samples WHERE user_feedback = 0").Scan(&incorrect)

	stats.CorrectCount = int(correct)
	stats.CorrectionCount = int(incorrect)

	if stats.TotalSamples > 0 {
		stats.AccuracyRate = float64(stats.CorrectCount) / float64(stats.TotalSamples) * 100
	}

	stats.ByFieldType = make(map[string]models.FieldStats)

	rows, _ := s.db.Query(`
		SELECT field_type, 
		       COUNT(*) as total,
		       SUM(CASE WHEN user_feedback = 1 THEN 1 ELSE 0 END) as correct
		FROM handwriting_samples
		GROUP BY field_type
	`)
	defer rows.Close()

	for rows.Next() {
		var fieldType string
		var total, correct int64
		rows.Scan(&fieldType, &total, &correct)

		accuracy := 0.0
		if total > 0 {
			accuracy = float64(correct) / float64(total) * 100
		}

		stats.ByFieldType[fieldType] = models.FieldStats{
			Total:    int(total),
			Correct:  int(correct),
			Accuracy: accuracy,
		}
	}

	return &stats, nil
}

func (s *SQLiteDB) UpdateDailyStats(processed int, success int, avgConfidence float64) {
	today := time.Now().Format("2006-01-02")

	_, err := s.db.Exec(`
		INSERT INTO daily_stats (date, total_processed, success_count, average_confidence)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(date) DO UPDATE SET
			total_processed = total_processed + excluded.total_processed,
			success_count = success_count + excluded.success_count,
			average_confidence = (average_confidence + excluded.average_confidence) / 2
	`, today, processed, success, avgConfidence)

	if err != nil {
		log.Printf("Failed to update daily stats: %v", err)
	}
}

func (s *SQLiteDB) GetDailyStats(date string) (map[string]interface{}, error) {
	var result struct {
		TotalProcessed    int
		SuccessCount      int
		AverageConfidence float64
	}

	err := s.db.QueryRow(`
		SELECT total_processed, success_count, average_confidence
		FROM daily_stats WHERE date = ?
	`, date).Scan(&result.TotalProcessed, &result.SuccessCount, &result.AverageConfidence)

	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"date":               date,
		"total_processed":    result.TotalProcessed,
		"success_count":      result.SuccessCount,
		"average_confidence": result.AverageConfidence,
	}, nil
}

func (s *SQLiteDB) Close() error {
	return s.db.Close()
}
