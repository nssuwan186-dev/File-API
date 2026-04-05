package handlers

import (
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"

	"hotel-ocr-system/internal/database"
	"hotel-ocr-system/internal/models"
	"hotel-ocr-system/internal/ocr"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type DocumentHandler struct {
	ocr *ocr.SmartOCR
	db  *database.SQLiteDB
}

func NewDocumentHandler(ocr *ocr.SmartOCR, db *database.SQLiteDB) *DocumentHandler {
	return &DocumentHandler{ocr: ocr, db: db}
}

func (h *DocumentHandler) ProcessDocument(c *gin.Context) {
	file, err := c.FormFile("document")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no file uploaded"})
		return
	}

	if !h.isValidFile(file.Filename) {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid file format. allowed: jpg, jpeg, png, webp",
		})
		return
	}

	if file.Size > 10*1024*1024 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file too large (max 10MB)"})
		return
	}

	src, err := file.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "cannot open file"})
		return
	}
	defer src.Close()

	imageData, err := io.ReadAll(src)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "cannot read file"})
		return
	}

	result, err := h.ocr.ProcessDocument(imageData, file.Filename)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, result)
}

func (h *DocumentHandler) BatchProcess(c *gin.Context) {
	form, err := c.MultipartForm()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid form data"})
		return
	}

	files := form.File["documents"]
	if len(files) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no files uploaded"})
		return
	}

	if len(files) > 10 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "maximum 10 files per batch"})
		return
	}

	var readers []io.Reader
	var fileNames []string

	for _, file := range files {
		if !h.isValidFile(file.Filename) {
			continue
		}
		src, _ := file.Open()
		defer src.Close()

		data, _ := io.ReadAll(src)
		readers = append(readers, strings.NewReader(string(data)))
		fileNames = append(fileNames, file.Filename)
	}

	result := h.ocr.ProcessBatch(readers, fileNames)
	c.JSON(http.StatusOK, result)
}

func (h *DocumentHandler) GetDocument(c *gin.Context) {
	id := c.Param("id")

	doc, err := h.db.GetExtraction(id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "document not found"})
		return
	}

	c.JSON(http.StatusOK, doc)
}

func (h *DocumentHandler) SaveFeedback(c *gin.Context) {
	var req models.FeedbackRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.FieldType == "" || req.RawText == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "field_type and raw_text are required"})
		return
	}

	sample := &database.HandwritingSample{
		ImageHash:     req.ImageHash,
		ExtractionID:  req.ExtractionID,
		RawText:       req.RawText,
		CorrectedText: req.CorrectedText,
		FieldType:     req.FieldType,
		UserNotes:     req.UserNotes,
	}

	if req.IsCorrect {
		correct := true
		sample.UserFeedback = &correct
		sample.CorrectedText = req.RawText
	} else {
		correct := false
		sample.UserFeedback = &correct
	}

	if contextJSON, err := json.Marshal(req.Context); err == nil {
		sample.ContextJSON = string(contextJSON)
	}

	if err := h.db.SaveFeedback(sample); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "feedback saved successfully",
		"id":      uuid.New().String(),
	})
}

func (h *DocumentHandler) GetFeedbackStats(c *gin.Context) {
	stats, err := h.db.GetFeedbackStats()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, stats)
}

func (h *DocumentHandler) ListRooms(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"message": "room list"})
}

func (h *DocumentHandler) GetRoomInfo(c *gin.Context) {
	number := c.Param("number")
	c.JSON(http.StatusOK, gin.H{"room": number})
}

func (h *DocumentHandler) GetDashboardStats(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"today_processed":    0,
		"week_processed":     0,
		"month_processed":    0,
		"average_confidence": 0,
	})
}

func (h *DocumentHandler) isValidFile(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	valid := []string{".jpg", ".jpeg", ".png", ".webp"}
	for _, v := range valid {
		if ext == v {
			return true
		}
	}
	return false
}
