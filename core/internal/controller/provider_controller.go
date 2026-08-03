package controller

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"warmnote/core/internal/model"
	"warmnote/core/internal/repository"
	"warmnote/core/internal/service"
)

const maxProviderRequestBody = 64 * 1024

type ProviderController struct {
	service *service.ProviderService
	logger  *slog.Logger
}

func NewProviderController(providerService *service.ProviderService, logger *slog.Logger) *ProviderController {
	return &ProviderController{service: providerService, logger: logger}
}

func (c *ProviderController) GetCatalog(response http.ResponseWriter, _ *http.Request) {
	writeJSON(response, http.StatusOK, map[string]any{"providers": c.service.Catalog()})
}

func (c *ProviderController) ListConfigurations(response http.ResponseWriter, _ *http.Request) {
	configurations, err := c.service.ListConfigurations()
	if err != nil {
		c.internalError(response, "list provider configurations", err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"configurations": configurations})
}

func (c *ProviderController) SaveConfiguration(response http.ResponseWriter, request *http.Request) {
	providerID := strings.TrimSpace(request.PathValue("providerID"))
	var input model.SaveProviderConfiguration
	decoder := json.NewDecoder(http.MaxBytesReader(response, request.Body, maxProviderRequestBody))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{"message": "请求内容无效"})
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeJSON(response, http.StatusBadRequest, map[string]string{"message": "请求只能包含一个 JSON 对象"})
		return
	}
	if input.ProviderID != "" && input.ProviderID != providerID {
		writeJSON(response, http.StatusBadRequest, map[string]string{"message": "Provider 标识不一致"})
		return
	}
	input.ProviderID = providerID

	configuration, err := c.service.SaveConfiguration(input)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidProviderConfiguration):
			writeJSON(response, http.StatusBadRequest, map[string]string{"message": strings.TrimPrefix(err.Error(), service.ErrInvalidProviderConfiguration.Error()+": ")})
		case errors.Is(err, service.ErrProviderNotFound):
			writeJSON(response, http.StatusNotFound, map[string]string{"message": "Provider 不存在"})
		default:
			c.internalError(response, "save provider configuration", err)
		}
		return
	}
	writeJSON(response, http.StatusOK, configuration)
}

func (c *ProviderController) DeleteConfiguration(response http.ResponseWriter, request *http.Request) {
	err := c.service.DeleteConfiguration(strings.TrimSpace(request.PathValue("providerID")))
	if err != nil {
		switch {
		case errors.Is(err, service.ErrProviderNotFound), errors.Is(err, repository.ErrProviderConfigurationNotFound):
			writeJSON(response, http.StatusNotFound, map[string]string{"message": "Provider 配置不存在"})
		default:
			c.internalError(response, "delete provider configuration", err)
		}
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (c *ProviderController) TestConfiguration(response http.ResponseWriter, request *http.Request) {
	providerID := strings.TrimSpace(request.PathValue("providerID"))
	var input model.TestProviderConfiguration
	decoder := json.NewDecoder(http.MaxBytesReader(response, request.Body, maxProviderRequestBody))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{"message": "请求内容无效"})
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeJSON(response, http.StatusBadRequest, map[string]string{"message": "请求只能包含一个 JSON 对象"})
		return
	}

	result, err := c.service.TestConfiguration(request.Context(), providerID, input)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidProviderConfiguration):
			writeJSON(response, http.StatusBadRequest, map[string]string{"message": strings.TrimPrefix(err.Error(), service.ErrInvalidProviderConfiguration.Error()+": ")})
		case errors.Is(err, service.ErrProviderNotFound):
			writeJSON(response, http.StatusNotFound, map[string]string{"message": "Provider 不存在"})
		default:
			c.logger.Warn("test provider configuration", "provider", providerID, "error", err)
			writeJSON(response, http.StatusBadGateway, map[string]string{"message": "无法连接 Provider，请检查 Base URL 和网络"})
		}
		return
	}
	writeJSON(response, http.StatusOK, result)
}

func (c *ProviderController) ListEnabledModels(response http.ResponseWriter, _ *http.Request) {
	models, err := c.service.EnabledModels()
	if err != nil {
		c.internalError(response, "list enabled models", err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"models": models})
}

func (c *ProviderController) internalError(response http.ResponseWriter, operation string, err error) {
	c.logger.Error(operation, "error", err)
	writeJSON(response, http.StatusInternalServerError, map[string]string{"message": "本地配置服务不可用"})
}
