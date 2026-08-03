package webserver

import (
	"net/http"

	"warmnote/core/internal/controller"
)

func NewRouter(runtimeController *controller.RuntimeController, providerController *controller.ProviderController) http.Handler {
	router := http.NewServeMux()
	router.HandleFunc("GET /api/v1/runtime", runtimeController.GetInfo)
	router.HandleFunc("GET /api/v1/model-catalog", providerController.GetCatalog)
	router.HandleFunc("GET /api/v1/agent-providers", providerController.ListConfigurations)
	router.HandleFunc("PUT /api/v1/agent-providers/{providerID}", providerController.SaveConfiguration)
	router.HandleFunc("DELETE /api/v1/agent-providers/{providerID}", providerController.DeleteConfiguration)
	router.HandleFunc("POST /api/v1/agent-providers/{providerID}/test", providerController.TestConfiguration)
	router.HandleFunc("GET /api/v1/models/enabled", providerController.ListEnabledModels)

	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Cache-Control", "no-store")
		response.Header().Set("X-Content-Type-Options", "nosniff")
		router.ServeHTTP(response, request)
	})
}
