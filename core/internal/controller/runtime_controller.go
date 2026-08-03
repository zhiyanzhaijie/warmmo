package controller

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"

	"warmnote/core/internal/service"
)

type RuntimeController struct {
	service *service.RuntimeService
	logger  *slog.Logger
}

func NewRuntimeController(runtimeService *service.RuntimeService, logger *slog.Logger) *RuntimeController {
	return &RuntimeController{service: runtimeService, logger: logger}
}

func (c *RuntimeController) GetInfo(response http.ResponseWriter, request *http.Request) {
	requestID, err := newRequestID()
	if err != nil {
		c.logger.Error("generate request id", "error", err)
		writeJSON(response, http.StatusInternalServerError, map[string]string{"message": "runtime unavailable"})
		return
	}

	writeJSON(response, http.StatusOK, c.service.GetInfo(request.Context(), requestID))
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
