package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"

	"warmmo/core/internal/application"
)

type RuntimeController struct {
	app    *application.RuntimeService
	logger *slog.Logger
}

func NewRuntimeController(runtimeService *application.RuntimeService, logger *slog.Logger) *RuntimeController {
	return &RuntimeController{app: runtimeService, logger: logger}
}

func (c *RuntimeController) GetInfo(response http.ResponseWriter, request *http.Request) {
	requestID, err := newRequestID()
	if err != nil {
		writeAppError(response, c.logger, "generate request id", err)
		return
	}

	writeJSON(response, http.StatusOK, c.app.GetInfo(request.Context(), requestID))
}

func newRequestID() (string, error) {
	buffer := make([]byte, 8)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return hex.EncodeToString(buffer), nil
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.WriteHeader(status)
	if err := json.NewEncoder(response).Encode(value); err != nil {
		slog.Error("write json response", "error", err)
	}
}
