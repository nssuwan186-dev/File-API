package ocr

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"hotel-ocr-system/internal/config"
	"hotel-ocr-system/internal/database"
	"hotel-ocr-system/internal/models"
	"hotel-ocr-system/pkg/thai"

	"github.com/agext/levenshtein"
	"github.com/google/uuid"
)

type SmartOCR struct {
	config      *config.Config
	gemini      *GeminiClient
	cloudVision *CloudVisionClient
	db          *database.SQLiteDB
	roomDB      map[string]thai.RoomInfo
	spelling    *thai.SpellingCorrector
}

func NewSmartOCR(cfg *config.Config, db *database.SQLiteDB) (*SmartOCR, error) {
	gemini, err := NewGeminiClient(cfg.GeminiAPIKey, cfg.OCRTimeout)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize Gemini: %w", err)
	}

	ocr := &SmartOCR{
		config:   cfg,
		gemini:   gemini,
		db:       db,
		roomDB:   loadRoomDatabase(cfg.RoomDataPath),
		spelling: thai.NewSpellingCorrector(),
	}

	ocr.loadVocabularyFromDB()

	if cfg.EnableCloudVision && cfg.GoogleCloudKey != "" {
		ocr.cloudVision = NewCloudVisionClient(cfg.GoogleCloudKey)
	}

	log.Printf("✅ SmartOCR initialized with %d rooms in database", len(ocr.roomDB))
	return ocr, nil
}

func (s *SmartOCR) ProcessDocument(imageData []byte, fileName string) (*models.ExtractedDocument, error) {
	start := time.Now()

	hash := md5.Sum(imageData)
	imageHash := hex.EncodeToString(hash[:])

	if cached := s.getFromCache(imageHash); cached != nil {
		log.Printf("📋 Cache hit for %s", fileName)
		return cached, nil
	}

	contextHints := s.buildContextHints()

	doc, err := s.gemini.ExtractDocument(imageData, contextHints)
	if err != nil {
		return nil, fmt.Errorf("extraction failed: %w", err)
	}

	s.enhanceDocument(doc)

	s.validateDocument(doc)

	doc.ID = uuid.New().String()
	doc.ImageHash = imageHash
	doc.FileName = fileName
	doc.ProcessingTime = time.Since(start).Milliseconds()
	doc.CreatedAt = time.Now()
	doc.Sources = map[string]string{"primary": "gemini-1.5-flash"}

	doc.Confidence = s.calculateOverallConfidence(doc)

	if err := s.db.SaveExtraction(doc); err != nil {
		log.Printf("⚠️ Failed to save extraction: %v", err)
	}

	s.db.UpdateDailyStats(1, 1, doc.Confidence)

	return doc, nil
}

func (s *SmartOCR) ProcessBatch(images []io.Reader, fileNames []string) *models.BatchResult {
	result := &models.BatchResult{
		TotalFiles: len(images),
	}

	start := time.Now()

	for i, r := range images {
		data, err := io.ReadAll(r)
		if err != nil {
			result.Failed = append(result.Failed, models.BatchError{
				FileName: fileNames[i],
				Error:    "cannot read file: " + err.Error(),
			})
			continue
		}

		doc, err := s.ProcessDocument(data, fileNames[i])
		if err != nil {
			result.Failed = append(result.Failed, models.BatchError{
				FileName: fileNames[i],
				Error:    err.Error(),
			})
		} else {
			result.Successful = append(result.Successful, doc)
		}
	}

	result.TotalTime = time.Since(start).Milliseconds()
	result.SuccessCount = len(result.Successful)
	result.FailCount = len(result.Failed)

	return result
}

func (s *SmartOCR) enhanceDocument(doc *models.ExtractedDocument) {
	s.enhanceName(&doc.GuestName)

	for i := range doc.RoomNumbers {
		s.enhanceRoom(&doc.RoomNumbers[i])
	}

	if doc.Phone.Value != "" {
		doc.Phone.Value = cleanPhoneNumber(doc.Phone.Value)
	}

	if doc.IDCard.Value != "" {
		doc.IDCard.Value = cleanIDCard(doc.IDCard.Value)
	}

	if doc.Nights.Value == "" && doc.CheckIn.Value != "" && doc.CheckOut.Value != "" {
		if nights := calculateNights(doc.CheckIn.Value, doc.CheckOut.Value); nights > 0 {
			doc.Nights.Value = strconv.Itoa(nights)
			doc.Nights.Confidence = "calculated"
		}
	}
}

func (s *SmartOCR) enhanceName(name *models.NameField) {
	if name.RawValue == "" {
		return
	}

	corrected := s.spelling.Correct(name.RawValue)
	if corrected != name.RawValue {
		name.PredictedValue = corrected
	}

	samples := s.db.FindSimilarNames(name.RawValue, 3)

	var matches []models.HistoricalMatch
	for _, sample := range samples {
		dist := levenshtein.Distance(name.RawValue, sample.CorrectedText, nil)
		maxLen := float64(max(len(name.RawValue), len(sample.CorrectedText)))
		similarity := 1.0 - (float64(dist) / maxLen)

		if similarity > 0.6 {
			matches = append(matches, models.HistoricalMatch{
				Name:       sample.CorrectedText,
				Confidence: similarity * 100,
				Source:     "database",
			})
		}
	}

	if len(matches) > 0 {
		name.HistoricalMatches = matches
		if matches[0].Confidence > 85 && name.ConfidenceLevel != "high" {
			name.PredictedValue = matches[0].Name
			name.Confidence = matches[0].Confidence
		}
	}

	if name.Confidence == 0 {
		switch name.ConfidenceLevel {
		case "high":
			name.Confidence = 90
		case "medium":
			name.Confidence = 70
		case "low":
			name.Confidence = 40
		default:
			name.Confidence = 50
		}
	}
}

func (s *SmartOCR) enhanceRoom(room *models.RoomInfo) {
	raw := strings.ToUpper(strings.TrimSpace(room.Original))

	clean := strings.NewReplacer(
		"O", "0",
		"I", "1",
		"L", "1",
		" ", "",
		"-", "",
	).Replace(raw)

	if info, ok := s.roomDB[clean]; ok {
		room.Corrected = clean
		room.Confidence = "high"
		room.MatchScore = 1.0
		room.Source = "exact"
		room.RoomData = map[string]interface{}{
			"building": info.Building,
			"floor":    info.Floor,
			"type":     info.Type,
			"price":    info.Price,
		}
		return
	}

	bestMatch, bestScore := s.findBestRoomMatch(clean)

	if bestScore > 0.7 {
		room.Corrected = bestMatch
		room.MatchScore = bestScore

		if bestScore > 0.9 {
			room.Confidence = "high"
		} else if bestScore > 0.8 {
			room.Confidence = "medium"
		} else {
			room.Confidence = "low"
		}
		room.Source = "fuzzy"

		if info, ok := s.roomDB[bestMatch]; ok {
			room.RoomData = map[string]interface{}{
				"building": info.Building,
				"floor":    info.Floor,
				"type":     info.Type,
				"price":    info.Price,
			}
		}

		room.Alternatives = s.findRoomAlternatives(clean, 2)
	} else {
		room.Corrected = clean
		room.Confidence = "uncertain"
		room.MatchScore = bestScore
		room.Source = "unknown"
	}
}

func (s *SmartOCR) findBestRoomMatch(input string) (string, float64) {
	bestMatch := ""
	bestScore := 0.0

	for roomNum, info := range s.roomDB {
		dist := levenshtein.Distance(input, roomNum, nil)
		maxLen := float64(max(len(input), len(roomNum)))
		score := 1.0 - (float64(dist) / maxLen)

		if len(input) > 0 && len(roomNum) > 0 && input[0] == roomNum[0] {
			score += 0.15
		}

		if len(input) >= 2 && len(roomNum) >= 2 {
			inputFloor := 0
			roomFloor := info.Floor
			if f, err := strconv.Atoi(input[1:2]); err == nil {
				inputFloor = f
			}
			if inputFloor == roomFloor {
				score += 0.1
			}
		}

		if score > bestScore {
			bestScore = score
			bestMatch = roomNum
		}
	}

	return bestMatch, bestScore
}

func (s *SmartOCR) findRoomAlternatives(input string, count int) []string {
	type scored struct {
		room  string
		score float64
	}

	var scores []scored
	for roomNum := range s.roomDB {
		dist := levenshtein.Distance(input, roomNum, nil)
		maxLen := float64(max(len(input), len(roomNum)))
		score := 1.0 - (float64(dist) / maxLen)
		scores = append(scores, scored{roomNum, score})
	}

	for i := 0; i < len(scores); i++ {
		for j := i + 1; j < len(scores); j++ {
			if scores[j].score > scores[i].score {
				scores[i], scores[j] = scores[j], scores[i]
			}
		}
	}

	var result []string
	for i := 1; i < len(scores) && len(result) < count; i++ {
		if scores[i].score > 0.5 {
			result = append(result, scores[i].room)
		}
	}

	return result
}

func (s *SmartOCR) validateDocument(doc *models.ExtractedDocument) {
	var validations []models.ValidationWarning

	if phone := doc.Phone.Value; phone != "" {
		clean := regexp.MustCompile(`\D`).ReplaceAllString(phone, "")
		if len(clean) != 10 {
			validations = append(validations, models.ValidationWarning{
				Type:     "format_error",
				Field:    "phone",
				Message:  fmt.Sprintf("เบอร์โทร '%s' ไม่ครบ 10 หลัก (พบ %d หลัก)", phone, len(clean)),
				Severity: "warning",
			})
		}
	}

	if id := doc.IDCard.Value; id != "" {
		clean := regexp.MustCompile(`\D`).ReplaceAllString(id, "")
		if len(clean) != 13 {
			validations = append(validations, models.ValidationWarning{
				Type:     "format_error",
				Field:    "id_card",
				Message:  fmt.Sprintf("เลขบัตรประชาชนไม่ครบ 13 หลัก (พบ %d หลัก)", len(clean)),
				Severity: "warning",
			})
		}
	}

	if len(doc.RoomNumbers) > 0 && doc.TotalAmount.Value != "" {
		amount, _ := strconv.Atoi(doc.TotalAmount.Value)
		expectedTotal := 0

		for _, room := range doc.RoomNumbers {
			if room.RoomData != nil {
				if price, ok := room.RoomData["price"].(int); ok {
					expectedTotal += price
				}
			}
		}

		nights, _ := strconv.Atoi(doc.Nights.Value)
		if nights <= 0 {
			nights = 1
		}
		expectedTotal *= nights

		if expectedTotal > 0 && amount != expectedTotal {
			diff := amount - expectedTotal
			if diff > 100 || diff < -100 {
				validations = append(validations, models.ValidationWarning{
					Type:      "price_discrepancy",
					Field:     "total_amount",
					Message:   fmt.Sprintf("ยอดรวม %d บาท ไม่ตรงกับราคาห้อง (%d บาท/คืน x %d คืน = %d)", amount, expectedTotal/nights, nights, expectedTotal),
					Suggested: expectedTotal,
					Severity:  "info",
				})
			}
		}
	}

	if doc.CheckIn.Value != "" && doc.CheckOut.Value == "" {
		validations = append(validations, models.ValidationWarning{
			Type:     "missing",
			Field:    "check_out",
			Message:  "ไม่พบวันที่เช็คเอาท์",
			Severity: "info",
		})
	}

	doc.Validations = validations
}

func (s *SmartOCR) calculateOverallConfidence(doc *models.ExtractedDocument) float64 {
	scores := []float64{}

	if doc.GuestName.Confidence > 0 {
		scores = append(scores, doc.GuestName.Confidence)
	}

	for _, room := range doc.RoomNumbers {
		switch room.Confidence {
		case "high":
			scores = append(scores, 90)
		case "medium":
			scores = append(scores, 70)
		case "low":
			scores = append(scores, 40)
		}
	}

	fields := []models.DocumentField{doc.CheckIn, doc.CheckOut, doc.Phone, doc.IDCard}
	for _, f := range fields {
		switch f.Confidence {
		case "high":
			scores = append(scores, 95)
		case "medium":
			scores = append(scores, 75)
		case "low":
			scores = append(scores, 50)
		}
	}

	if len(scores) == 0 {
		return 50
	}

	sum := 0.0
	for _, sc := range scores {
		sum += sc
	}
	return sum / float64(len(scores))
}

func (s *SmartOCR) buildContextHints() string {
	corrections := s.db.GetRecentCorrections(5)
	if len(corrections) == 0 {
		return ""
	}

	var hints strings.Builder
	hints.WriteString("\nตัวอย่างการแก้ไขล่าสุด:\n")
	for _, c := range corrections {
		hints.WriteString(fmt.Sprintf("- '%s' → '%s' (%s)\n", c.RawText, c.CorrectedText, c.FieldType))
	}

	return hints.String()
}

func (s *SmartOCR) getFromCache(imageHash string) *models.ExtractedDocument {
	return nil
}

func (s *SmartOCR) loadVocabularyFromDB() {
}

func (s *SmartOCR) Close() error {
	if s.gemini != nil {
		return s.gemini.Close()
	}
	return nil
}

func cleanPhoneNumber(phone string) string {
	re := regexp.MustCompile(`\D`)
	return re.ReplaceAllString(phone, "")
}

func cleanIDCard(id string) string {
	re := regexp.MustCompile(`\D`)
	return re.ReplaceAllString(id, "")
}

func calculateNights(checkIn, checkOut string) int {
	layout := "02-01-06"
	t1, err1 := time.Parse(layout, checkIn)
	t2, err2 := time.Parse(layout, checkOut)

	if err1 != nil || err2 != nil {
		return 0
	}

	duration := t2.Sub(t1)
	return int(duration.Hours() / 24)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func loadRoomDatabase(path string) map[string]thai.RoomInfo {
	if data, err := os.ReadFile(path); err == nil {
		var rooms map[string]thai.RoomInfo
		if err := json.Unmarshal(data, &rooms); err == nil {
			return rooms
		}
	}

	return map[string]thai.RoomInfo{
		"B101": {Building: "B", Floor: 1, Type: "Standard", Price: 400},
		"B102": {Building: "B", Floor: 1, Type: "Standard", Price: 400},
		"B103": {Building: "B", Floor: 1, Type: "Standard", Price: 400},
		"B104": {Building: "B", Floor: 1, Type: "Standard", Price: 400},
		"B105": {Building: "B", Floor: 1, Type: "Standard", Price: 400},
		"B106": {Building: "B", Floor: 1, Type: "Standard", Price: 400},
		"B107": {Building: "B", Floor: 1, Type: "Standard", Price: 400},
		"B108": {Building: "B", Floor: 1, Type: "Standard", Price: 400},
		"B109": {Building: "B", Floor: 1, Type: "Standard", Price: 400},
		"B110": {Building: "B", Floor: 1, Type: "Standard", Price: 400},
		"B111": {Building: "B", Floor: 1, Type: "Standard Twin", Price: 500},
		"A101": {Building: "A", Floor: 1, Type: "Standard", Price: 400},
		"A102": {Building: "A", Floor: 1, Type: "Standard", Price: 400},
		"A103": {Building: "A", Floor: 1, Type: "Standard", Price: 400},
		"A104": {Building: "A", Floor: 1, Type: "Standard", Price: 400},
		"A105": {Building: "A", Floor: 1, Type: "Standard", Price: 400},
		"A106": {Building: "A", Floor: 1, Type: "Standard Twin", Price: 500},
		"A107": {Building: "A", Floor: 1, Type: "Standard Twin", Price: 500},
		"A108": {Building: "A", Floor: 1, Type: "Standard Twin", Price: 500},
		"A109": {Building: "A", Floor: 1, Type: "Standard Twin", Price: 500},
		"A110": {Building: "A", Floor: 1, Type: "Standard Twin", Price: 500},
		"A111": {Building: "A", Floor: 1, Type: "Standard", Price: 400},
		"N1":   {Building: "N", Floor: 1, Type: "Standard Twin", Price: 600},
		"N2":   {Building: "N", Floor: 1, Type: "Standard", Price: 500},
		"N3":   {Building: "N", Floor: 1, Type: "Standard", Price: 500},
		"N4":   {Building: "N", Floor: 1, Type: "Standard Twin", Price: 600},
		"N5":   {Building: "N", Floor: 1, Type: "Standard Twin", Price: 600},
		"N6":   {Building: "N", Floor: 1, Type: "Standard Twin", Price: 600},
		"N7":   {Building: "N", Floor: 1, Type: "Standard", Price: 500},
	}
}
