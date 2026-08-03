package webserver

import (
	"net/http"

	"warmnote/core/internal/controller"
)

func NewRouter(runtimeController *controller.RuntimeController) http.Handler {
	router := http.NewServeMux()
	router.HandleFunc("GET /api/v1/runtime", runtimeController.GetInfo)

	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Cache-Control", "no-store")
		response.Header().Set("X-Content-Type-Options", "nosniff")
		router.ServeHTTP(response, request)
	})
}
