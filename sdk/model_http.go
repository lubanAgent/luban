package luban

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

const maxModelResponseBytes = 16 << 20

type modelTransportError struct{ cause error }

func (e modelTransportError) Error() string { return "model HTTP transport failed" }
func (e modelTransportError) Unwrap() error { return e.cause }

func (m *protocolModel) post(ctx *gin.Context, endpoint string, payload any, response any) (resultErr error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			resultErr = fmt.Errorf("model HTTPDoer panicked")
		}
	}()
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode model request: %w", err)
	}
	stdCtx, err := contextForGin(ctx)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(stdCtx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("construct model request")
	}
	request.Header.Set("Content-Type", "application/json")
	if m.connection.Protocol == "anthropic" {
		request.Header.Set("x-api-key", m.connection.APIKey)
		request.Header.Set("anthropic-version", "2023-06-01")
	} else {
		request.Header.Set("Authorization", "Bearer "+m.connection.APIKey)
	}
	result, err := m.doer.Do(request)
	if err != nil {
		if result != nil && result.Body != nil {
			_ = result.Body.Close()
		}
		// Doers may include credentials or request dumps in their errors.
		// Preserve cancellation semantics without copying transport diagnostics.
		if cancelErr := stdCtx.Err(); cancelErr != nil {
			return fmt.Errorf("model HTTP request: %w", cancelErr)
		}
		return modelTransportError{cause: err}
	}
	if result == nil || result.Body == nil {
		return fmt.Errorf("model HTTP response is empty")
	}
	defer result.Body.Close()
	if result.StatusCode < 200 || result.StatusCode >= 300 {
		return fmt.Errorf("model HTTP status %d", result.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(result.Body, maxModelResponseBytes+1))
	if err != nil {
		return fmt.Errorf("read model HTTP response")
	}
	if len(raw) > maxModelResponseBytes {
		return fmt.Errorf("model HTTP response exceeds %d bytes", maxModelResponseBytes)
	}
	if err := json.Unmarshal(raw, response); err != nil {
		return fmt.Errorf("invalid model JSON response")
	}
	return nil
}

func modelEndpoint(baseURL, protocol string) string {
	base := strings.TrimRight(baseURL, "/")
	if protocol == "anthropic" {
		if strings.HasSuffix(base, "/messages") {
			return base
		}
		if strings.HasSuffix(base, "/v1") {
			return base + "/messages"
		}
		return base + "/v1/messages"
	}
	if strings.HasSuffix(base, "/chat/completions") {
		return base
	}
	return base + "/chat/completions"
}
