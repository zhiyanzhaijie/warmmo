package controller

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"warmnote/core/internal/canvas"
	"warmnote/core/internal/service"
)

const maxCanvasRequestBody = 1024 * 1024

type CanvasController struct {
	service *service.CanvasService
	logger  *slog.Logger
}

type createCanvasNodeRequest struct {
	Kind    string  `json:"kind"`
	Title   string  `json:"title"`
	Content string  `json:"content"`
	X       float64 `json:"x"`
	Y       float64 `json:"y"`
}

type getCanvasNodesRequest struct {
	NodeIDs []string `json:"nodeIds"`
}

type updateCanvasNodePositionRequest struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

func NewCanvasController(canvasService *service.CanvasService, logger *slog.Logger) *CanvasController {
	return &CanvasController{service: canvasService, logger: logger}
}

func (c *CanvasController) CreateNode(response http.ResponseWriter, request *http.Request) {
	var input createCanvasNodeRequest
	if !decodeCanvasRequest(response, request, &input) {
		return
	}
	node, err := c.service.CreateNode(request.Context(), canvas.CreateNodeInput{
		WorkID: request.PathValue("workID"), Kind: input.Kind, Title: input.Title, Content: input.Content,
		X: input.X, Y: input.Y,
	})
	if errors.Is(err, service.ErrInvalidCanvasRequest) {
		writeJSON(response, http.StatusBadRequest, map[string]string{"message": "节点类型、标题和内容不能为空"})
		return
	}
	if err != nil {
		c.internalError(response, "create canvas node", err)
		return
	}
	writeJSON(response, http.StatusCreated, node)
}

func (c *CanvasController) UpdateNodePosition(response http.ResponseWriter, request *http.Request) {
	var input updateCanvasNodePositionRequest
	if !decodeCanvasRequest(response, request, &input) {
		return
	}
	err := c.service.UpdateNodePosition(request.Context(), request.PathValue("workID"), request.PathValue("nodeID"), input.X, input.Y)
	if errors.Is(err, canvas.ErrNodeNotFound) {
		writeJSON(response, http.StatusNotFound, map[string]string{"message": "画布节点不存在"})
		return
	}
	if err != nil {
		c.internalError(response, "update canvas node position", err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (c *CanvasController) ListNodes(response http.ResponseWriter, request *http.Request) {
	nodes, err := c.service.ListNodes(request.Context(), request.PathValue("workID"))
	if err != nil {
		c.internalError(response, "list canvas nodes", err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"nodes": nodes})
}

func (c *CanvasController) GetNodes(response http.ResponseWriter, request *http.Request) {
	var input getCanvasNodesRequest
	if !decodeCanvasRequest(response, request, &input) {
		return
	}
	nodes, err := c.service.GetNodes(request.Context(), request.PathValue("workID"), input.NodeIDs)
	switch {
	case errors.Is(err, service.ErrInvalidCanvasRequest):
		writeJSON(response, http.StatusBadRequest, map[string]string{"message": "至少选择一个节点"})
	case errors.Is(err, canvas.ErrNodeNotFound):
		writeJSON(response, http.StatusNotFound, map[string]string{"message": "画布节点不存在"})
	case err != nil:
		c.internalError(response, "get canvas nodes", err)
	default:
		writeJSON(response, http.StatusOK, map[string]any{"nodes": nodes})
	}
}

func (c *CanvasController) ListCandidates(response http.ResponseWriter, request *http.Request) {
	candidates, err := c.service.ListCandidates(request.Context(), request.PathValue("workID"))
	if err != nil {
		c.internalError(response, "list canvas candidates", err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"candidates": candidates})
}

func (c *CanvasController) internalError(response http.ResponseWriter, operation string, err error) {
	c.logger.Error(operation, "error", err)
	writeJSON(response, http.StatusInternalServerError, map[string]string{"message": "画布服务不可用"})
}

func decodeCanvasRequest(response http.ResponseWriter, request *http.Request, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(response, request.Body, maxCanvasRequestBody))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{"message": "请求内容无效"})
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeJSON(response, http.StatusBadRequest, map[string]string{"message": "请求只能包含一个 JSON 对象"})
		return false
	}
	return true
}
