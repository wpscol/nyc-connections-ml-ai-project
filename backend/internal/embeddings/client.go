package embeddings

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// Client calls an OpenAI-compatible embeddings endpoint (e.g. LM Studio).
type Client struct {
	baseURL      string
	model        string // empty = auto-discover from /v1/models
	resolvedModel string
	resolveOnce  sync.Once
	apiKey       string
	http         *http.Client
}

// NewClient creates a Client. baseURL is the server root (no /v1 suffix),
// e.g. "http://localhost:1234". model may be empty to auto-discover the
// first loaded model from /v1/models. apiKey may be empty for local servers.
func NewClient(baseURL, model, apiKey string) *Client {
	return &Client{
		baseURL: baseURL,
		model:   model,
		apiKey:  apiKey,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

type modelsResponse struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

// resolveModel returns the model name to use, discovering it from /v1/models
// on first call if the configured model is empty.
func (c *Client) resolveModel(ctx context.Context) (string, error) {
	if c.model != "" {
		return c.model, nil
	}
	var resolveErr error
	c.resolveOnce.Do(func() {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/models", nil)
		if err != nil {
			resolveErr = fmt.Errorf("build models request: %w", err)
			return
		}
		if c.apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+c.apiKey)
		}
		resp, err := c.http.Do(req)
		if err != nil {
			resolveErr = fmt.Errorf("models request: %w", err)
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			resolveErr = fmt.Errorf("models API returned HTTP %d", resp.StatusCode)
			return
		}
		var result modelsResponse
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			resolveErr = fmt.Errorf("decode models response: %w", err)
			return
		}
		if len(result.Data) == 0 {
			resolveErr = fmt.Errorf("no models loaded in LM Studio")
			return
		}
		c.resolvedModel = result.Data[0].ID
	})
	if resolveErr != nil {
		// reset so next call retries
		c.resolveOnce = sync.Once{}
		return "", resolveErr
	}
	return c.resolvedModel, nil
}

func (c *Client) Model() string {
	if c.model != "" {
		return c.model
	}
	return c.resolvedModel // may be empty before first Embed call
}

// BaseURL returns the configured LLM server root (no /v1 suffix).
func (c *Client) BaseURL() string { return c.baseURL }

// ListModels queries GET /v1/models and returns the loaded model IDs.
// Used by the health check to confirm the LLM server is reachable.
func (c *Client) ListModels(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/models", nil)
	if err != nil {
		return nil, err
	}
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("models API returned HTTP %d", resp.StatusCode)
	}
	var result modelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(result.Data))
	for _, m := range result.Data {
		ids = append(ids, m.ID)
	}
	return ids, nil
}

type embedRequest struct {
	Input []string `json:"input"`
	Model string   `json:"model"`
}

type embedResponse struct {
	Data []struct {
		Embedding []float64 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
}

// Embed returns one embedding vector per text, preserving input order.
func (c *Client) Embed(ctx context.Context, texts []string) ([][]float64, error) {
	model, err := c.resolveModel(ctx)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(embedRequest{Input: texts, Model: model})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embed request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embed API returned HTTP %d", resp.StatusCode)
	}
	var result embedResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode embed response: %w", err)
	}

	out := make([][]float64, len(texts))
	for _, d := range result.Data {
		if d.Index >= 0 && d.Index < len(out) {
			out[d.Index] = d.Embedding
		}
	}
	for i, v := range out {
		if v == nil {
			return nil, fmt.Errorf("missing embedding for index %d", i)
		}
	}
	return out, nil
}
