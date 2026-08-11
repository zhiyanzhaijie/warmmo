package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"warmmo/core/internal/application"
	"warmmo/core/internal/domain/ai"
)

const maxProviderRequestBody = 64 * 1024

type ProviderController struct {
	app    *application.ProviderService
	logger *slog.Logger
}

func NewProviderController(providerService *application.ProviderService, logger *slog.Logger) *ProviderController {
	return &ProviderController{app: providerService, logger: logger}
}

func (c *ProviderController) GetCatalog(response http.ResponseWriter, _ *http.Request) {
	writeJSON(response, http.StatusOK, map[string]any{"providers": c.app.Catalog()})
}

func (c *ProviderController) ListConfigurations(response http.ResponseWriter, _ *http.Request) {
	configurations, err := c.app.ListConfigurations()
	if err != nil {
		writeAppError(response, c.logger, "list provider configurations", err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"configurations": configurations})
}

func (c *ProviderController) SaveConfiguration(response http.ResponseWriter, request *http.Request) {
	providerID := strings.TrimSpace(request.PathValue("providerID"))
	var input ai.SaveProviderConfiguration
	decoder := json.NewDecoder(http.MaxBytesReader(response, request.Body, maxProviderRequestBody))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeInvalidRequest(response, "INVALID_REQUEST_BODY", "请求内容无效", err)
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeInvalidRequest(response, "INVALID_REQUEST_BODY", "请求只能包含一个 JSON 对象", err)
		return
	}
	if input.ProviderID != "" && input.ProviderID != providerID {
		writeInvalidRequest(response, "PROVIDER_ID_MISMATCH", "Provider 标识不一致", nil)
		return
	}
	input.ProviderID = providerID

	configuration, err := c.app.SaveConfiguration(input)
	if err != nil {
		writeAppError(response, c.logger, "save provider configuration", err)
		return
	}
	writeJSON(response, http.StatusOK, configuration)
}

func (c *ProviderController) DeleteConfiguration(response http.ResponseWriter, request *http.Request) {
	err := c.app.DeleteConfiguration(strings.TrimSpace(request.PathValue("providerID")))
	if err != nil {
		writeAppError(response, c.logger, "delete provider configuration", err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (c *ProviderController) TestConfiguration(response http.ResponseWriter, request *http.Request) {
	providerID := strings.TrimSpace(request.PathValue("providerID"))
	var input ai.TestProviderConfiguration
	decoder := json.NewDecoder(http.MaxBytesReader(response, request.Body, maxProviderRequestBody))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeInvalidRequest(response, "INVALID_REQUEST_BODY", "请求内容无效", err)
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeInvalidRequest(response, "INVALID_REQUEST_BODY", "请求只能包含一个 JSON 对象", err)
		return
	}

	result, err := c.app.TestConfiguration(request.Context(), providerID, input)
	if err != nil {
		writeAppError(response, c.logger, "test provider configuration", err)
		return
	}
	writeJSON(response, http.StatusOK, result)
}

func (c *ProviderController) ListEnabledModels(response http.ResponseWriter, _ *http.Request) {
	models, err := c.app.EnabledModels()
	if err != nil {
		writeAppError(response, c.logger, "list enabled models", err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"models": models})
}
