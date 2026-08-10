package embedding

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type OpenAICompatibleConfig struct {
	BaseURL    string
	APIKey     string
	ModelID    string
	Dimensions int
	HTTPClient *http.Client
}

type OpenAICompatible struct {
	config OpenAICompatibleConfig
}

func NewOpenAICompatible(config OpenAICompatibleConfig) (*OpenAICompatible, error) {
	config.BaseURL = strings.TrimRight(strings.TrimSpace(config.BaseURL), "/")
	config.ModelID = strings.TrimSpace(config.ModelID)
	if config.BaseURL == "" || config.ModelID == "" || config.Dimensions < 1 {
		return nil, errors.New("embedding base URL, model ID, and dimensions are required")
	}
	if config.HTTPClient == nil {
		config.HTTPClient = &http.Client{Timeout: 2 * time.Minute}
	}
	return &OpenAICompatible{config: config}, nil
}

func (e *OpenAICompatible) ModelID() string {
	return e.config.ModelID
}

func (e *OpenAICompatible) Dimensions() int {
	return e.config.Dimensions
}

func (e *OpenAICompatible) Embed(ctx context.Context, text string) ([]float32, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, errors.New("embedding input is empty")
	}
	payload, err := json.Marshal(map[string]any{
		"model": e.config.ModelID, "input": text,
		"dimensions": e.config.Dimensions, "encoding_format": "float",
	})
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, e.config.BaseURL+"/embeddings", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+e.config.APIKey)
	request.Header.Set("Content-Type", "application/json")
	response, err := e.config.HTTPClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("call embedding provider: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 16*1024))
		return nil, fmt.Errorf("embedding provider returned HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	var decoded struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
			Index     int       `json:"index"`
		} `json:"data"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error,omitempty"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 8*1024*1024)).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("decode embedding response: %w", err)
	}
	if decoded.Error != nil {
		return nil, errors.New(decoded.Error.Message)
	}
	actualDimensions := 0
	if len(decoded.Data) > 0 {
		actualDimensions = len(decoded.Data[0].Embedding)
	}
	if len(decoded.Data) != 1 || actualDimensions != e.config.Dimensions {
		return nil, fmt.Errorf("embedding provider returned %d vectors with dimension %d", len(decoded.Data), actualDimensions)
	}
	return decoded.Data[0].Embedding, nil
}
