package ocr

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"hotel-ocr-system/internal/models"

	"github.com/google/generative-ai-go/genai"
	"google.golang.org/api/option"
)

type GeminiClient struct {
	client  *genai.Client
	model   *genai.GenerativeModel
	timeout time.Duration
}

func NewGeminiClient(apiKey string, timeoutSeconds int) (*GeminiClient, error) {
	ctx := context.Background()
	client, err := genai.NewClient(ctx, option.WithAPIKey(apiKey))
	if err != nil {
		return nil, fmt.Errorf("failed to create Gemini client: %w", err)
	}

	model := client.GenerativeModel("gemini-1.5-flash-002")

	model.SetTemperature(0.1)
	model.SetTopP(0.95)
	model.SetTopK(40)

	return &GeminiClient{
		client:  client,
		model:   model,
		timeout: time.Duration(timeoutSeconds) * time.Second,
	}, nil
}

func (g *GeminiClient) ExtractDocument(imageData []byte, contextHints string) (*models.ExtractedDocument, error) {
	ctx, cancel := context.WithTimeout(context.Background(), g.timeout)
	defer cancel()

	prompt := g.buildPrompt(contextHints)

	parts := []genai.Part{
		genai.ImageData("image/jpeg", imageData),
		genai.Text(prompt),
	}

	resp, err := g.model.GenerateContent(ctx, parts...)
	if err != nil {
		return nil, fmt.Errorf("Gemini API error: %w", err)
	}

	text := extractTextFromResponse(resp)
	return g.parseResponse(text)
}

func (g *GeminiClient) buildPrompt(contextHints string) string {
	return fmt.Sprintf(`คุณเป็นระบบ OCR สำหรับใบเสร็จโรงแรม VIPAT RUNGKAN HOTEL

%s

กฎการอ่านลายมือไทยที่สำคัญ:
1. ตัว ก อาจเขียนคล้าย ถ (หัว ถ สูงกว่า) หรือ ภ (มีส่วนโค้ง)
2. ตัว น อาจเขียนคล้าย ม (ตัว ม มีหัวโค้งชัดเจน) หรือ ห
3. ตัว ส อาจเขียนคล้าย ข หรือ ช (ดูจากหางตัวอักษร)
4. สระ อำ อาจเขียนเหมือน สระ อา + ตัว ม ติดกัน
5. ตัวเลข: 0 อาจเหมือน O, 1 อาจเหมือน I หรือ 7, 5 อาจเหมือน 6

โครงสร้างเอกสาร:
- มุมบนขวา: เวลาเข้าพัก
- กลางบน: วันที่เข้าพัก (วันที่เช็คอิน) และวันที่เช็คเอาท์
- ช่องใหญ่: ชื่อ-นามสกุล (ลายมือ)
- ด้านล่าง: เลขบัตรประชาชน, เบอร์โทร, ทะเบียนรถ
- ตาราง: รายการห้องพักและราคา
- ล่างสุด: หมายเลขห้อง, ลงชื่อผู้เข้าพัก

ส่งคืน JSON ตามโครงสร้างนี้เท่านั้น (ไม่ต้องมี markdown):
{
  "check_in": {"value": "DD-MM-YY หรือ null", "confidence": "high/medium/low", "raw_reading": "ข้อความดิบ"},
  "check_out": {"value": "...", "confidence": "...", "raw_reading": "..."},
  "nights": {"value": "ตัวเลข หรือ null", "confidence": "...", "raw_reading": "..."},
  "guest_name": {
    "value": "ชื่อที่อ่านได้", 
    "confidence": "...",
    "raw_reading": "...",
    "character_breakdown": [
      {"char": "ตัวอักษร", "possible": ["ตัวเลือก1", "ตัวเลือก2"], "confidence": 0.9}
    ]
  },
  "id_card": {"value": "เลข 13 หลัก ไม่มีขีด", "confidence": "...", "raw_reading": "..."},
  "phone": {"value": "เบอร์ 10 หลัก", "confidence": "...", "raw_reading": "..."},
  "license_plate": {"value": "...", "confidence": "...", "raw_reading": "..."},
  "room_numbers": [
    {"raw": "B107", "predicted": "B107", "confidence": 0.9, "alternatives": ["B108", "B109"]}
  ],
  "room_type": {"value": "Standard/Standard Twin", "confidence": "...", "raw_reading": "..."},
  "payment_method": {"value": "เงินสด/โอนบัญชี/บัตรเครดิต", "confidence": "...", "raw_reading": "..."},
  "total_amount": {"value": "ตัวเลข", "confidence": "...", "raw_reading": "..."}
}

หมายเหตุ:
- ถ้าช่องไหนว่าง ให้ใส่ null ใน value
- confidence high = ตัวพิมพ์หรือลายมือชัดมาก
- confidence medium = ลายมือพออ่านได้ มีความไม่แน่ใจบ้าง
- confidence low = ลายมือเลือน หรืออ่านยาก`, contextHints)
}

func (g *GeminiClient) parseResponse(text string) (*models.ExtractedDocument, error) {
	jsonStr := extractJSON(text)

	var raw struct {
		CheckIn   models.DocumentField `json:"check_in"`
		CheckOut  models.DocumentField `json:"check_out"`
		Nights    models.DocumentField `json:"nights"`
		GuestName struct {
			Value              string                `json:"value"`
			Confidence         string                `json:"confidence"`
			RawReading         string                `json:"raw_reading"`
			CharacterBreakdown []models.CharAnalysis `json:"character_breakdown"`
		} `json:"guest_name"`
		IDCard       models.DocumentField `json:"id_card"`
		Phone        models.DocumentField `json:"phone"`
		LicensePlate models.DocumentField `json:"license_plate"`
		RoomNumbers  []struct {
			Raw          string   `json:"raw"`
			Predicted    string   `json:"predicted"`
			Confidence   float64  `json:"confidence"`
			Alternatives []string `json:"alternatives"`
		} `json:"room_numbers"`
		RoomType      models.DocumentField `json:"room_type"`
		PaymentMethod models.DocumentField `json:"payment_method"`
		TotalAmount   models.DocumentField `json:"total_amount"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &raw); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w\nRaw: %s", err, jsonStr)
	}

	doc := &models.ExtractedDocument{
		CheckIn:       raw.CheckIn,
		CheckOut:      raw.CheckOut,
		Nights:        raw.Nights,
		IDCard:        raw.IDCard,
		Phone:         raw.Phone,
		LicensePlate:  raw.LicensePlate,
		RoomType:      raw.RoomType,
		PaymentMethod: raw.PaymentMethod,
		TotalAmount:   raw.TotalAmount,
		GuestName: models.NameField{
			RawValue:           raw.GuestName.Value,
			PredictedValue:     raw.GuestName.Value,
			ConfidenceLevel:    raw.GuestName.Confidence,
			CharacterBreakdown: raw.GuestName.CharacterBreakdown,
		},
	}

	for _, r := range raw.RoomNumbers {
		confidence := "medium"
		if r.Confidence > 0.8 {
			confidence = "high"
		} else if r.Confidence < 0.5 {
			confidence = "low"
		}

		doc.RoomNumbers = append(doc.RoomNumbers, models.RoomInfo{
			Original:     r.Raw,
			Corrected:    r.Predicted,
			Confidence:   confidence,
			MatchScore:   r.Confidence,
			Alternatives: r.Alternatives,
		})
	}

	return doc, nil
}

func extractTextFromResponse(resp *genai.GenerateContentResponse) string {
	var result strings.Builder
	for _, cand := range resp.Candidates {
		if cand.Content != nil {
			for _, part := range cand.Content.Parts {
				if text, ok := part.(genai.Text); ok {
					result.WriteString(string(text))
				}
			}
		}
	}
	return result.String()
}

func extractJSON(text string) string {
	patterns := []string{
		"(?s)```json\\s*(.*?)\\s*```",
		"(?s)```\\s*(.*?)\\s*```",
	}

	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)
		if matches := re.FindStringSubmatch(text); len(matches) > 1 {
			return strings.TrimSpace(matches[1])
		}
	}

	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start != -1 && end != -1 && end > start {
		return text[start : end+1]
	}

	return text
}

func (g *GeminiClient) Close() error {
	return g.client.Close()
}
