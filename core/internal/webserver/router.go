package webserver

import (
	"net/http"

	"warmnote/core/internal/controller"
)

func NewRouter(runtimeController *controller.RuntimeController, providerController *controller.ProviderController, workController *controller.WorkController, agentController *controller.AgentController, canvasController *controller.CanvasController) http.Handler {
	router := http.NewServeMux()
	router.HandleFunc("GET /api/v1/runtime", runtimeController.GetInfo)
	router.HandleFunc("GET /api/v1/model-catalog", providerController.GetCatalog)
	router.HandleFunc("GET /api/v1/agent-providers", providerController.ListConfigurations)
	router.HandleFunc("PUT /api/v1/agent-providers/{providerID}", providerController.SaveConfiguration)
	router.HandleFunc("DELETE /api/v1/agent-providers/{providerID}", providerController.DeleteConfiguration)
	router.HandleFunc("POST /api/v1/agent-providers/{providerID}/test", providerController.TestConfiguration)
	router.HandleFunc("GET /api/v1/models/enabled", providerController.ListEnabledModels)
	router.HandleFunc("GET /api/v1/works", workController.List)
	router.HandleFunc("POST /api/v1/works", workController.Create)
	router.HandleFunc("GET /api/v1/works/{workID}", workController.Get)
	router.HandleFunc("PATCH /api/v1/works/{workID}", workController.Update)
	router.HandleFunc("GET /api/v1/work-folders", workController.ListFolders)
	router.HandleFunc("POST /api/v1/work-folders", workController.CreateFolder)
	router.HandleFunc("POST /api/v1/works/{workID}/agent-runs", agentController.CreateRun)
	router.HandleFunc("GET /api/v1/agent-runs/{runID}", agentController.GetRun)
	router.HandleFunc("GET /api/v1/agent-runs/{runID}/events", agentController.StreamEvents)
	router.HandleFunc("POST /api/v1/agent-runs/{runID}/cancel", agentController.CancelRun)
	router.HandleFunc("POST /api/v1/works/{workID}/nodes", canvasController.CreateNode)
	router.HandleFunc("DELETE /api/v1/works/{workID}/nodes", canvasController.DeleteNodes)
	router.HandleFunc("PATCH /api/v1/works/{workID}/nodes/positions", canvasController.UpdateNodePositions)
	router.HandleFunc("GET /api/v1/works/{workID}/nodes", canvasController.ListNodes)
	router.HandleFunc("POST /api/v1/works/{workID}/nodes/query", canvasController.GetNodes)
	router.HandleFunc("GET /api/v1/works/{workID}/nodes/{nodeID}", canvasController.GetNode)
	router.HandleFunc("PATCH /api/v1/works/{workID}/nodes/{nodeID}", canvasController.UpdateNode)
	router.HandleFunc("PATCH /api/v1/works/{workID}/nodes/{nodeID}/position", canvasController.UpdateNodePosition)
	router.HandleFunc("GET /api/v1/works/{workID}/canvas-history", canvasController.GetHistoryState)
	router.HandleFunc("POST /api/v1/works/{workID}/canvas-history/undo", canvasController.Undo)
	router.HandleFunc("POST /api/v1/works/{workID}/canvas-history/redo", canvasController.Redo)
	router.HandleFunc("GET /api/v1/works/{workID}/edges", canvasController.ListEdges)
	router.HandleFunc("GET /api/v1/works/{workID}/candidates", canvasController.ListCandidates)
	router.HandleFunc("PATCH /api/v1/works/{workID}/candidates/{candidateID}/position", canvasController.UpdateCandidatePosition)
	router.HandleFunc("POST /api/v1/works/{workID}/candidates/{candidateID}/accept", canvasController.AcceptCandidate)
	router.HandleFunc("POST /api/v1/works/{workID}/candidates/{candidateID}/reject", canvasController.RejectCandidate)

	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Cache-Control", "no-store")
		response.Header().Set("X-Content-Type-Options", "nosniff")
		router.ServeHTTP(response, request)
	})
}
