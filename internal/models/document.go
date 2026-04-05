package models

import (
	"time"
)

type DocumentField struct {
	Value        string   `json:"value"`
	Confidence   string   `json:"confidence"`
	RawReading   string   `json:"raw_reading,omitempty"`
	Alternatives []string `json:"alternatives,omitempty"`
	FieldType    string   `json:"field_type,omitempty"`
}

type NameField struct {
	RawValue           string            `json:"raw_value"`
	PredictedValue     string            `json:"predicted_value"`
	Confidence         float64           `json:"confidence_score"`
	ConfidenceLevel    string            `json:"confidence"`
	HistoricalMatches  []HistoricalMatch `json:"historical_matches,omitempty"`
	CharacterBreakdown []CharAnalysis    `json:"character_analysis,omitempty"`
}

type CharAnalysis struct {
	Character  string   `json:"char"`
	Possible   []string `json:"possible"`
	Confidence float64  `json:"confidence"`
}

type HistoricalMatch struct {
	Name       string  `json:"name"`
	Confidence float64 `json:"confidence_score"`
	Source     string  `json:"source"`
}

type RoomInfo struct {
	Original     string                 `json:"original"`
	Corrected    string                 `json:"corrected"`
	Confidence   string                 `json:"confidence"`
	MatchScore   float64                `json:"match_score,omitempty"`
	Alternatives []string               `json:"alternatives,omitempty"`
	RoomData     map[string]interface{} `json:"room_data,omitempty"`
	Source       string                 `json:"source"`
}

type ValidationWarning struct {
	Type      string      `json:"type"`
	Field     string      `json:"field"`
	Message   string      `json:"message"`
	Suggested interface{} `json:"suggested_value,omitempty"`
	Severity  string      `json:"severity"`
}

type ExtractedDocument struct {
	ID        string `json:"id"`
	FileName  string `json:"file_name"`
	ImageHash string `json:"image_hash"`

	CheckIn      DocumentField `json:"check_in"`
	CheckOut     DocumentField `json:"check_out"`
	Nights       DocumentField `json:"nights"`
	GuestName    NameField     `json:"guest_name"`
	IDCard       DocumentField `json:"id_card"`
	Phone        DocumentField `json:"phone"`
	LicensePlate DocumentField `json:"license_plate"`

	RoomNumbers []RoomInfo    `json:"room_numbers"`
	RoomType    DocumentField `json:"room_type"`

	PaymentMethod DocumentField `json:"payment_method"`
	TotalAmount   DocumentField `json:"total_amount"`

	Validations []ValidationWarning `json:"validations"`
	Confidence  float64             `json:"overall_confidence"`

	ProcessingTime int64     `json:"processing_time_ms"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`

	Sources map[string]string `json:"sources,omitempty"`
}

type BatchResult struct {
	Successful   []*ExtractedDocument `json:"successful"`
	Failed       []BatchError         `json:"failed"`
	TotalFiles   int                  `json:"total_files"`
	SuccessCount int                  `json:"success_count"`
	FailCount    int                  `json:"fail_count"`
	TotalTime    int64                `json:"total_time_ms"`
}

type BatchError struct {
	FileName string `json:"file_name"`
	Error    string `json:"error"`
}

type FeedbackRequest struct {
	ImageHash     string                 `json:"image_hash"`
	ExtractionID  string                 `json:"extraction_id,omitempty"`
	FieldType     string                 `json:"field_type"`
	RawText       string                 `json:"raw_text"`
	CorrectedText string                 `json:"corrected_text"`
	IsCorrect     bool                   `json:"is_correct"`
	UserNotes     string                 `json:"user_notes,omitempty"`
	Context       map[string]interface{} `json:"context,omitempty"`
}

type FeedbackStats struct {
	TotalSamples    int                   `json:"total_samples"`
	CorrectCount    int                   `json:"correct_count"`
	CorrectionCount int                   `json:"correction_count"`
	AccuracyRate    float64               `json:"accuracy_rate"`
	ByFieldType     map[string]FieldStats `json:"by_field_type"`
}

type FieldStats struct {
	Total    int     `json:"total"`
	Correct  int     `json:"correct"`
	Accuracy float64 `json:"accuracy"`
}

type DashboardStats struct {
	TodayProcessed    int                 `json:"today_processed"`
	WeekProcessed     int                 `json:"week_processed"`
	MonthProcessed    int                 `json:"month_processed"`
	AverageConfidence float64             `json:"average_confidence"`
	TopCorrections    []CorrectionPattern `json:"top_corrections"`
	RoomUsage         map[string]int      `json:"room_usage"`
}

type CorrectionPattern struct {
	From      string `json:"from"`
	To        string `json:"to"`
	Frequency int    `json:"frequency"`
}
