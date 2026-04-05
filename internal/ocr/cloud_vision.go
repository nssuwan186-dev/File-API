package ocr

import (
	"fmt"
)

type CloudVisionClient struct {
	client interface{}
}

func NewCloudVisionClient(apiKey string) *CloudVisionClient {
	return &CloudVisionClient{}
}

func (c *CloudVisionClient) DetectText(imageData []byte) (*VisionResult, error) {
	return nil, fmt.Errorf("cloud vision not initialized")
}

type VisionResult struct {
	FullText string
	Blocks   []TextBlock
}

type TextBlock struct {
	Text       string
	Confidence float64
}
