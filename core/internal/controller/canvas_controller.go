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
	Kind           string   `json:"kind"`
	Title          string   `json:"title"`
	Content        string   `json:"content"`
	X              float64  `json:"x"`
	Y              float64  `json:"y"`
	ContextNodeIDs []string `json:"contextNodeIds"`
}

type createCanvasEdgeRequest struct {
	SourceNodeID string `json:"sourceNodeId"`
	TargetNodeID string `json:"targetNodeId"`
}

type deleteCanvasEdgesRequest struct {
	EdgeIDs []string `json:"edgeIds"`
}

type getCanvasNodesRequest struct {
	NodeIDs []string `json:"nodeIds"`
}

type updateCanvasNodePositionRequest struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

type updateCanvasNodePositionsRequest struct {
	Positions []canvas.NodePosition `json:"positions"`
}

type deleteCanvasNodesRequest struct {
	NodeIDs []string `json:"nodeIds"`
}

type updateCanvasNodeRequest struct {
	Title            string `json:"title"`
	Content          string `json:"content"`
	ExpectedRevision int64  `json:"expectedRevision"`
}

type acceptCanvasCandidateRequest struct {
	Title string `json:"title"`
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
		WorkID: request.PathValue("workID"), Kind: canvas.NodeKind(input.Kind), Title: input.Title, Content: input.Content,
		X: input.X, Y: input.Y, ContextNodeIDs: input.ContextNodeIDs,
	})
	if errors.Is(err, service.ErrInvalidCanvasRequest) {
		writeJSON(response, http.StatusBadRequest, map[string]string{"message": "作品 ID、节点类型或标题无效"})
		return
	}
	if errors.Is(err, canvas.ErrNodeNotFound) {
		writeJSON(response, http.StatusNotFound, map[string]string{"message": "部分上下文节点不存在"})
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

func (c *CanvasController) UpdateNodePositions(response http.ResponseWriter, request *http.Request) {
	var input updateCanvasNodePositionsRequest
	if !decodeCanvasRequest(response, request, &input) {
		return
	}
	err := c.service.UpdateNodePositions(request.Context(), request.PathValue("workID"), input.Positions)
	switch {
	case errors.Is(err, service.ErrInvalidCanvasRequest), errors.Is(err, canvas.ErrInvalidNode):
		writeJSON(response, http.StatusBadRequest, map[string]string{"message": "节点位置请求无效"})
	case errors.Is(err, canvas.ErrNodeNotFound):
		writeJSON(response, http.StatusNotFound, map[string]string{"message": "画布节点不存在"})
	case err != nil:
		c.internalError(response, "update canvas node positions", err)
	default:
		response.WriteHeader(http.StatusNoContent)
	}
}

func (c *CanvasController) DeleteNodes(response http.ResponseWriter, request *http.Request) {
	var input deleteCanvasNodesRequest
	if !decodeCanvasRequest(response, request, &input) {
		return
	}
	err := c.service.DeleteNodes(request.Context(), request.PathValue("workID"), input.NodeIDs)
	switch {
	case errors.Is(err, service.ErrInvalidCanvasRequest), errors.Is(err, canvas.ErrInvalidNode):
		writeJSON(response, http.StatusBadRequest, map[string]string{"message": "请选择 1 到 100 个节点"})
	case errors.Is(err, canvas.ErrNodeNotFound):
		writeJSON(response, http.StatusNotFound, map[string]string{"message": "部分画布节点不存在"})
	case err != nil:
		c.internalError(response, "delete canvas nodes", err)
	default:
		response.WriteHeader(http.StatusNoContent)
	}
}

func (c *CanvasController) GetHistoryState(response http.ResponseWriter, request *http.Request) {
	state, err := c.service.GetHistoryState(request.Context(), request.PathValue("workID"))
	if errors.Is(err, service.ErrInvalidCanvasRequest) {
		writeJSON(response, http.StatusBadRequest, map[string]string{"message": "作品 ID 不能为空"})
		return
	}
	if err != nil {
		c.internalError(response, "read canvas history", err)
		return
	}
	writeJSON(response, http.StatusOK, state)
}

func (c *CanvasController) Undo(response http.ResponseWriter, request *http.Request) {
	c.moveHistory(response, request, false)
}

func (c *CanvasController) Redo(response http.ResponseWriter, request *http.Request) {
	c.moveHistory(response, request, true)
}

func (c *CanvasController) moveHistory(response http.ResponseWriter, request *http.Request, forward bool) {
	var (
		state canvas.HistoryState
		err   error
	)
	if forward {
		state, err = c.service.Redo(request.Context(), request.PathValue("workID"))
	} else {
		state, err = c.service.Undo(request.Context(), request.PathValue("workID"))
	}
	switch {
	case errors.Is(err, service.ErrInvalidCanvasRequest):
		writeJSON(response, http.StatusBadRequest, map[string]string{"message": "作品 ID 不能为空"})
	case errors.Is(err, canvas.ErrHistoryUnavailable):
		writeJSON(response, http.StatusConflict, map[string]string{"message": "没有可执行的历史操作"})
	case errors.Is(err, canvas.ErrNodeNotFound):
		writeJSON(response, http.StatusConflict, map[string]string{"message": "历史操作涉及的节点状态已变化"})
	case err != nil:
		c.internalError(response, "move canvas history", err)
	default:
		writeJSON(response, http.StatusOK, state)
	}
}

func (c *CanvasController) ListNodes(response http.ResponseWriter, request *http.Request) {
	nodes, err := c.service.ListNodes(request.Context(), request.PathValue("workID"))
	if err != nil {
		c.internalError(response, "list canvas nodes", err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"nodes": nodes})
}

func (c *CanvasController) GetNode(response http.ResponseWriter, request *http.Request) {
	node, err := c.service.GetNode(
		request.Context(),
		request.PathValue("workID"),
		request.PathValue("nodeID"),
	)
	switch {
	case errors.Is(err, service.ErrInvalidCanvasRequest):
		writeJSON(response, http.StatusBadRequest, map[string]string{"message": "节点请求无效"})
	case errors.Is(err, canvas.ErrNodeNotFound):
		writeJSON(response, http.StatusNotFound, map[string]string{"message": "画布节点不存在"})
	case err != nil:
		c.internalError(response, "get canvas node", err)
	default:
		writeJSON(response, http.StatusOK, node)
	}
}

func (c *CanvasController) UpdateNode(response http.ResponseWriter, request *http.Request) {
	var input updateCanvasNodeRequest
	if !decodeCanvasRequest(response, request, &input) {
		return
	}
	node, err := c.service.UpdateNode(request.Context(), canvas.UpdateNodeInput{
		WorkID:           request.PathValue("workID"),
		NodeID:           request.PathValue("nodeID"),
		Title:            input.Title,
		Content:          input.Content,
		ExpectedRevision: input.ExpectedRevision,
	})
	switch {
	case errors.Is(err, service.ErrInvalidCanvasRequest):
		writeJSON(response, http.StatusBadRequest, map[string]string{"message": "标题或 Revision 无效"})
	case errors.Is(err, canvas.ErrNodeNotFound):
		writeJSON(response, http.StatusNotFound, map[string]string{"message": "画布节点不存在"})
	case errors.Is(err, canvas.ErrRevisionConflict):
		writeJSON(response, http.StatusConflict, map[string]string{"message": "节点已在其他位置更新，请重新加载后再保存"})
	case err != nil:
		c.internalError(response, "update canvas node", err)
	default:
		writeJSON(response, http.StatusOK, node)
	}
}

func (c *CanvasController) ListEdges(response http.ResponseWriter, request *http.Request) {
	edges, err := c.service.ListEdges(request.Context(), request.PathValue("workID"))
	if errors.Is(err, service.ErrInvalidCanvasRequest) {
		writeJSON(response, http.StatusBadRequest, map[string]string{"message": "作品 ID 不能为空"})
		return
	}
	if err != nil {
		c.internalError(response, "list canvas edges", err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"edges": edges})
}

func (c *CanvasController) CreateEdge(response http.ResponseWriter, request *http.Request) {
	var input createCanvasEdgeRequest
	if !decodeCanvasRequest(response, request, &input) {
		return
	}
	edge, err := c.service.CreateEdge(request.Context(), canvas.CreateEdgeInput{
		WorkID: request.PathValue("workID"), SourceNodeID: input.SourceNodeID, TargetNodeID: input.TargetNodeID,
	})
	switch {
	case errors.Is(err, service.ErrInvalidCanvasRequest):
		writeJSON(response, http.StatusBadRequest, map[string]string{"message": "连接节点无效"})
	case errors.Is(err, canvas.ErrNodeNotFound):
		writeJSON(response, http.StatusNotFound, map[string]string{"message": "连接节点不存在"})
	case err != nil:
		c.internalError(response, "create canvas edge", err)
	default:
		writeJSON(response, http.StatusCreated, edge)
	}
}

func (c *CanvasController) DeleteEdges(response http.ResponseWriter, request *http.Request) {
	var input deleteCanvasEdgesRequest
	if !decodeCanvasRequest(response, request, &input) {
		return
	}
	err := c.service.DeleteEdges(request.Context(), request.PathValue("workID"), input.EdgeIDs)
	switch {
	case errors.Is(err, service.ErrInvalidCanvasRequest):
		writeJSON(response, http.StatusBadRequest, map[string]string{"message": "请选择 1 到 100 条连接"})
	case errors.Is(err, canvas.ErrNodeNotFound):
		writeJSON(response, http.StatusNotFound, map[string]string{"message": "部分连接不存在"})
	case err != nil:
		c.internalError(response, "delete canvas edges", err)
	default:
		response.WriteHeader(http.StatusNoContent)
	}
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

func (c *CanvasController) UpdateCandidatePosition(response http.ResponseWriter, request *http.Request) {
	var input updateCanvasNodePositionRequest
	if !decodeCanvasRequest(response, request, &input) {
		return
	}
	err := c.service.UpdateCandidatePosition(
		request.Context(),
		request.PathValue("workID"),
		request.PathValue("candidateID"),
		input.X,
		input.Y,
	)
	switch {
	case errors.Is(err, service.ErrInvalidCanvasRequest):
		writeJSON(response, http.StatusBadRequest, map[string]string{"message": "Candidate 请求无效"})
	case errors.Is(err, canvas.ErrCandidateNotFound):
		writeJSON(response, http.StatusNotFound, map[string]string{"message": "Candidate 不存在"})
	case errors.Is(err, canvas.ErrCandidateResolved):
		writeJSON(response, http.StatusConflict, map[string]string{"message": "Candidate 已处理，不能再移动"})
	case err != nil:
		c.internalError(response, "update canvas candidate position", err)
	default:
		response.WriteHeader(http.StatusNoContent)
	}
}

func (c *CanvasController) AcceptCandidate(response http.ResponseWriter, request *http.Request) {
	var input acceptCanvasCandidateRequest
	if !decodeCanvasRequest(response, request, &input) {
		return
	}
	node, err := c.service.AcceptCandidate(request.Context(), canvas.AcceptCandidateInput{
		WorkID:      request.PathValue("workID"),
		CandidateID: request.PathValue("candidateID"),
		Title:       input.Title,
	})
	switch {
	case errors.Is(err, service.ErrInvalidCanvasRequest):
		writeJSON(response, http.StatusBadRequest, map[string]string{"message": "Candidate 请求无效"})
	case errors.Is(err, canvas.ErrCandidateNotFound):
		writeJSON(response, http.StatusNotFound, map[string]string{"message": "Candidate 不存在"})
	case errors.Is(err, canvas.ErrCandidateResolved):
		writeJSON(response, http.StatusConflict, map[string]string{"message": "Candidate 已被丢弃，不能接受"})
	case err != nil:
		c.internalError(response, "accept canvas candidate", err)
	default:
		writeJSON(response, http.StatusOK, node)
	}
}

func (c *CanvasController) RejectCandidate(response http.ResponseWriter, request *http.Request) {
	err := c.service.RejectCandidate(
		request.Context(),
		request.PathValue("workID"),
		request.PathValue("candidateID"),
	)
	switch {
	case errors.Is(err, service.ErrInvalidCanvasRequest):
		writeJSON(response, http.StatusBadRequest, map[string]string{"message": "Candidate 请求无效"})
	case errors.Is(err, canvas.ErrCandidateNotFound):
		writeJSON(response, http.StatusNotFound, map[string]string{"message": "Candidate 不存在"})
	case errors.Is(err, canvas.ErrCandidateResolved):
		writeJSON(response, http.StatusConflict, map[string]string{"message": "Candidate 已接受，不能丢弃"})
	case err != nil:
		c.internalError(response, "reject canvas candidate", err)
	default:
		response.WriteHeader(http.StatusNoContent)
	}
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
