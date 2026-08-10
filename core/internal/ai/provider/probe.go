package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"warmnote/core/internal/ai"
)

type Probe struct {
	httpClient *http.Client
}

func NewProbe(httpClient *http.Client) *Probe {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 8 * time.Second}
	}
	return &Probe{httpClient: httpClient}
}

func (p *Probe) Test(ctx context.Context, baseURL, apiKey, modelID string, capability ai.ModelCapability) (ai.ProviderTestResult, error) {
	if capability == ai.ModelCapabilityEmbedding {
		return p.testEmbedding(ctx, baseURL, apiKey, modelID)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/models", nil)
	if err != nil {
		return ai.ProviderTestResult{}, fmt.Errorf("create provider test request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+apiKey)

	startedAt := time.Now()
	response, err := p.httpClient.Do(request)
	latencyMS := time.Since(startedAt).Milliseconds()
	if err != nil {
		return ai.ProviderTestResult{}, fmt.Errorf("request provider: %w", err)
	}
	defer response.Body.Close()

	switch {
	case response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices:
		return ai.ProviderTestResult{Valid: true, Message: "API Key 有效，Provider 连接正常", LatencyMS: latencyMS}, nil
	case response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden:
		return ai.ProviderTestResult{Valid: false, Message: "API Key 无效或没有访问权限", LatencyMS: latencyMS}, nil
	default:
		return ai.ProviderTestResult{Valid: false, Message: fmt.Sprintf("Provider 返回 HTTP %d", response.StatusCode), LatencyMS: latencyMS}, nil
	}
}

func (p *Probe) testEmbedding(ctx context.Context, baseURL, apiKey, modelID string) (ai.ProviderTestResult, error) {
	payload, err := json.Marshal(map[string]any{
		"model": modelID, "input": "Warmnote embedding connection test",
		"dimensions": ai.CanonicalEmbeddingDimensions, "encoding_format": "float",
	})
	if err != nil {
		return ai.ProviderTestResult{}, fmt.Errorf("encode embedding test request: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/embeddings", bytes.NewReader(payload))
	if err != nil {
		return ai.ProviderTestResult{}, fmt.Errorf("create embedding test request: %w", err)
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+apiKey)
	startedAt := time.Now()
	response, err := p.httpClient.Do(request)
	latencyMS := time.Since(startedAt).Milliseconds()
	if err != nil {
		return ai.ProviderTestResult{}, fmt.Errorf("request embedding provider: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusUnauthorized || response.StatusCode == http.StatusForbidden {
		return ai.ProviderTestResult{Valid: false, Message: "API Key 无效或没有访问权限", LatencyMS: latencyMS}, nil
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 16*1024))
		return ai.ProviderTestResult{Valid: false, Message: fmt.Sprintf("Embedding Provider 返回 HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(body))), LatencyMS: latencyMS}, nil
	}
	var decoded struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 2*1024*1024)).Decode(&decoded); err != nil {
		return ai.ProviderTestResult{Valid: false, Message: "Embedding Provider 返回内容无法解析", LatencyMS: latencyMS}, nil
	}
	if len(decoded.Data) != 1 || len(decoded.Data[0].Embedding) != ai.CanonicalEmbeddingDimensions {
		actualDimensions := 0
		if len(decoded.Data) == 1 {
			actualDimensions = len(decoded.Data[0].Embedding)
		}
		return ai.ProviderTestResult{Valid: false, Message: fmt.Sprintf("Embedding 返回维度为 %d，需要 %d", actualDimensions, ai.CanonicalEmbeddingDimensions), LatencyMS: latencyMS}, nil
	}
	return ai.ProviderTestResult{Valid: true, Message: "Embedding API Key 有效，模型连接正常（1024 维）", LatencyMS: latencyMS}, nil
}
