package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"warmmo/core/internal/application"
	"warmmo/core/internal/domain/canvas"
)

const maxCanvasRequestBody = 1024 * 1024

type CanvasController struct {
	app    *application.CanvasService
	logger *slog.Logger
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

func NewCanvasController(canvasService *application.CanvasService, logger *slog.Logger) *CanvasController {
	return &CanvasController{app: canvasService, logger: logger}
}

func (c *CanvasController) CreateNode(response http.ResponseWriter, request *http.Request) {
	var input createCanvasNodeRequest
	if !decodeCanvasRequest(response, request, &input) {
		return
	}
	node, err := c.app.CreateNode(request.Context(), canvas.CreateNodeInput{
		WorkID: request.PathValue("workID"), Kind: canvas.NodeKind(input.Kind), Title: input.Title, Content: input.Content,
		X: input.X, Y: input.Y, ContextNodeIDs: input.ContextNodeIDs,
	})
	if err != nil {
		writeAppError(response, c.logger, "create canvas node", err)
		return
	}
	writeJSON(response, http.StatusCreated, node)
}

func (c *CanvasController) UpdateNodePosition(response http.ResponseWriter, request *http.Request) {
	var input updateCanvasNodePositionRequest
	if !decodeCanvasRequest(response, request, &input) {
		return
	}
	err := c.app.UpdateNodePosition(request.Context(), request.PathValue("workID"), request.PathValue("nodeID"), input.X, input.Y)
	if err != nil {
		writeAppError(response, c.logger, "update canvas node position", err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (c *CanvasController) UpdateNodePositions(response http.ResponseWriter, request *http.Request) {
	var input updateCanvasNodePositionsRequest
	if !decodeCanvasRequest(response, request, &input) {
		return
	}
	err := c.app.UpdateNodePositions(request.Context(), request.PathValue("workID"), input.Positions)
	if err != nil {
		writeAppError(response, c.logger, "update canvas node positions", err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (c *CanvasController) LayoutChapter(response http.ResponseWriter, request *http.Request) {
	positions, err := c.app.LayoutChapter(
		request.Context(),
		request.PathValue("workID"),
		request.PathValue("nodeID"),
	)
	if err != nil {
		writeAppError(response, c.logger, "layout chapter nodes", err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"positions": positions})
}

func (c *CanvasController) DeleteNodes(response http.ResponseWriter, request *http.Request) {
	var input deleteCanvasNodesRequest
	if !decodeCanvasRequest(response, request, &input) {
		return
	}
	err := c.app.DeleteNodes(request.Context(), request.PathValue("workID"), input.NodeIDs)
	if err != nil {
		writeAppError(response, c.logger, "delete canvas nodes", err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (c *CanvasController) GetHistoryState(response http.ResponseWriter, request *http.Request) {
	state, err := c.app.GetHistoryState(request.Context(), request.PathValue("workID"))
	if err != nil {
		writeAppError(response, c.logger, "read canvas history", err)
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
		state, err = c.app.Redo(request.Context(), request.PathValue("workID"))
	} else {
		state, err = c.app.Undo(request.Context(), request.PathValue("workID"))
	}
	if err != nil {
		writeAppError(response, c.logger, "move canvas history", err)
		return
	}
	writeJSON(response, http.StatusOK, state)
}

func (c *CanvasController) ListNodes(response http.ResponseWriter, request *http.Request) {
	nodes, err := c.app.ListNodes(request.Context(), request.PathValue("workID"))
	if err != nil {
		writeAppError(response, c.logger, "list canvas nodes", err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"nodes": nodes})
}

func (c *CanvasController) GetNode(response http.ResponseWriter, request *http.Request) {
	node, err := c.app.GetNode(
		request.Context(),
		request.PathValue("workID"),
		request.PathValue("nodeID"),
	)
	if err != nil {
		writeAppError(response, c.logger, "get canvas node", err)
		return
	}
	writeJSON(response, http.StatusOK, node)
}

func (c *CanvasController) ListNodeVersions(response http.ResponseWriter, request *http.Request) {
	versions, err := c.app.ListNodeVersions(request.Context(), request.PathValue("workID"), request.PathValue("nodeID"))
	if err != nil {
		writeAppError(response, c.logger, "list canvas node versions", err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"versions": versions})
}

func (c *CanvasController) ListCurrentChapterArchives(response http.ResponseWriter, request *http.Request) {
	archives, err := c.app.ListChapterArchiveVisibility(request.Context(), request.PathValue("workID"))
	if err != nil {
		writeAppError(response, c.logger, "list current chapter archives", err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"archives": archives})
}

func (c *CanvasController) ListStorySpine(response http.ResponseWriter, request *http.Request) {
	pageable, err := parsePagination(request)
	if err != nil {
		writeAppError(response, c.logger, "parse story spine pagination", err)
		return
	}
	page, err := c.app.ListChapterArchiveTimelinePage(
		request.Context(),
		request.PathValue("workID"),
		pageable,
	)
	if err != nil {
		writeAppError(response, c.logger, "list story spine", err)
		return
	}
	writeJSON(response, http.StatusOK, page)
}

func (c *CanvasController) ListChapterArchiveHistory(response http.ResponseWriter, request *http.Request) {
	archives, err := c.app.ListChapterArchiveHistory(request.Context(), request.PathValue("workID"), request.PathValue("nodeID"))
	if err != nil {
		writeAppError(response, c.logger, "list chapter archive history", err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"archives": archives})
}

func (c *CanvasController) RetractChapterArchive(response http.ResponseWriter, request *http.Request) {
	err := c.app.RetractChapterArchive(request.Context(), request.PathValue("workID"), request.PathValue("archiveID"))
	if err != nil {
		writeAppError(response, c.logger, "retract chapter archive", err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (c *CanvasController) SwitchNodeVersion(response http.ResponseWriter, request *http.Request) {
	var input struct {
		VersionID string `json:"versionId"`
	}
	if !decodeCanvasRequest(response, request, &input) {
		return
	}
	node, err := c.app.SwitchNodeVersion(request.Context(), request.PathValue("workID"), request.PathValue("nodeID"), input.VersionID)
	if err != nil {
		writeAppError(response, c.logger, "switch canvas node version", err)
		return
	}
	writeJSON(response, http.StatusOK, node)
}

func (c *CanvasController) UpdateNode(response http.ResponseWriter, request *http.Request) {
	var input updateCanvasNodeRequest
	if !decodeCanvasRequest(response, request, &input) {
		return
	}
	node, err := c.app.UpdateNode(request.Context(), canvas.UpdateNodeInput{
		WorkID:           request.PathValue("workID"),
		NodeID:           request.PathValue("nodeID"),
		Title:            input.Title,
		Content:          input.Content,
		ExpectedRevision: input.ExpectedRevision,
	})
	if err != nil {
		writeAppError(response, c.logger, "update canvas node", err)
		return
	}
	writeJSON(response, http.StatusOK, node)
}

func (c *CanvasController) ListEdges(response http.ResponseWriter, request *http.Request) {
	edges, err := c.app.ListEdges(request.Context(), request.PathValue("workID"))
	if err != nil {
		writeAppError(response, c.logger, "list canvas edges", err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"edges": edges})
}

func (c *CanvasController) CreateEdge(response http.ResponseWriter, request *http.Request) {
	var input createCanvasEdgeRequest
	if !decodeCanvasRequest(response, request, &input) {
		return
	}
	edge, err := c.app.CreateEdge(request.Context(), canvas.CreateEdgeInput{
		WorkID: request.PathValue("workID"), SourceNodeID: input.SourceNodeID, TargetNodeID: input.TargetNodeID,
	})
	if err != nil {
		writeAppError(response, c.logger, "create canvas edge", err)
		return
	}
	writeJSON(response, http.StatusCreated, edge)
}

func (c *CanvasController) DeleteEdges(response http.ResponseWriter, request *http.Request) {
	var input deleteCanvasEdgesRequest
	if !decodeCanvasRequest(response, request, &input) {
		return
	}
	err := c.app.DeleteEdges(request.Context(), request.PathValue("workID"), input.EdgeIDs)
	if err != nil {
		writeAppError(response, c.logger, "delete canvas edges", err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (c *CanvasController) GetNodes(response http.ResponseWriter, request *http.Request) {
	var input getCanvasNodesRequest
	if !decodeCanvasRequest(response, request, &input) {
		return
	}
	nodes, err := c.app.GetNodes(request.Context(), request.PathValue("workID"), input.NodeIDs)
	if err != nil {
		writeAppError(response, c.logger, "get canvas nodes", err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"nodes": nodes})
}

func (c *CanvasController) ListCandidates(response http.ResponseWriter, request *http.Request) {
	candidates, err := c.app.ListCandidates(request.Context(), request.PathValue("workID"))
	if err != nil {
		writeAppError(response, c.logger, "list canvas candidates", err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"candidates": candidates})
}

func (c *CanvasController) UpdateCandidatePosition(response http.ResponseWriter, request *http.Request) {
	var input updateCanvasNodePositionRequest
	if !decodeCanvasRequest(response, request, &input) {
		return
	}
	err := c.app.UpdateCandidatePosition(
		request.Context(),
		request.PathValue("workID"),
		request.PathValue("candidateID"),
		input.X,
		input.Y,
	)
	if err != nil {
		writeAppError(response, c.logger, "update canvas candidate position", err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (c *CanvasController) AcceptCandidate(response http.ResponseWriter, request *http.Request) {
	var input acceptCanvasCandidateRequest
	if !decodeCanvasRequest(response, request, &input) {
		return
	}
	node, err := c.app.AcceptCandidate(request.Context(), canvas.AcceptCandidateInput{
		WorkID:      request.PathValue("workID"),
		CandidateID: request.PathValue("candidateID"),
		Title:       input.Title,
	})
	if err != nil {
		writeAppError(response, c.logger, "accept canvas candidate", err)
		return
	}
	writeJSON(response, http.StatusOK, node)
}

func (c *CanvasController) RejectCandidate(response http.ResponseWriter, request *http.Request) {
	err := c.app.RejectCandidate(
		request.Context(),
		request.PathValue("workID"),
		request.PathValue("candidateID"),
	)
	if err != nil {
		writeAppError(response, c.logger, "reject canvas candidate", err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func decodeCanvasRequest(response http.ResponseWriter, request *http.Request, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(response, request.Body, maxCanvasRequestBody))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeInvalidRequest(response, "INVALID_REQUEST_BODY", "请求内容无效", err)
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeInvalidRequest(response, "INVALID_REQUEST_BODY", "请求只能包含一个 JSON 对象", err)
		return false
	}
	return true
}
