package httpapi

import (
	"log/slog"
	"net/http"

	"warmmo/core/internal/application"
)

type errorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeAppError(response http.ResponseWriter, logger *slog.Logger, operation string, err error) {
	appErr := application.ToAppError(operation, err)
	status := statusForError(appErr.Kind())
	if status >= http.StatusInternalServerError {
		logger.Error(operation, "kind", appErr.Kind(), "code", appErr.Code(), "error", appErr)
	}
	writeJSON(response, status, errorResponse{Code: appErr.Code(), Message: appErr.PublicMessage()})
}

func writeInvalidRequest(response http.ResponseWriter, code, message string, cause error) {
	err := application.InvalidError(code, message, cause)
	writeJSON(response, http.StatusBadRequest, errorResponse{Code: err.Code(), Message: err.PublicMessage()})
}

func statusForError(kind application.ErrorKind) int {
	switch kind {
	case application.ErrorInvalid:
		return http.StatusBadRequest
	case application.ErrorNotFound:
		return http.StatusNotFound
	case application.ErrorConflict:
		return http.StatusConflict
	case application.ErrorLocked:
		return http.StatusLocked
	case application.ErrorUpstream:
		return http.StatusBadGateway
	default:
		return http.StatusInternalServerError
	}
}
