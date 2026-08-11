package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"warmmo/core/internal/application"
	"warmmo/core/internal/domain/work"
)

const maxWorkRequestBody = 16 * 1024

type WorkController struct {
	app    *application.WorkService
	logger *slog.Logger
}

type createWorkRequest struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	FolderID    string `json:"folderId"`
}

type updateWorkRequest struct {
	Title            string `json:"title"`
	Description      string `json:"description"`
	FolderID         string `json:"folderId"`
	Status           string `json:"status"`
	ExpectedRevision int64  `json:"expectedRevision"`
}

type createWorkFolderRequest struct {
	Name string `json:"name"`
}

func NewWorkController(workService *application.WorkService, logger *slog.Logger) *WorkController {
	return &WorkController{app: workService, logger: logger}
}

func (c *WorkController) List(response http.ResponseWriter, request *http.Request) {
	works, err := c.app.List(request.Context())
	if err != nil {
		writeAppError(response, c.logger, "list works", err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"works": works})
}

func (c *WorkController) Get(response http.ResponseWriter, request *http.Request) {
	detail, err := c.app.Get(request.Context(), request.PathValue("workID"))
	if err != nil {
		writeAppError(response, c.logger, "get work", err)
		return
	}
	writeJSON(response, http.StatusOK, detail)
}

func (c *WorkController) Create(response http.ResponseWriter, request *http.Request) {
	var input createWorkRequest
	decoder := json.NewDecoder(http.MaxBytesReader(response, request.Body, maxWorkRequestBody))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeInvalidRequest(response, "INVALID_REQUEST_BODY", "请求内容无效", err)
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeInvalidRequest(response, "INVALID_REQUEST_BODY", "请求只能包含一个 JSON 对象", err)
		return
	}

	created, err := c.app.Create(request.Context(), work.CreateInput{
		Title: input.Title, Description: input.Description, FolderID: input.FolderID,
	})
	if err != nil {
		writeAppError(response, c.logger, "create work", err)
		return
	}
	writeJSON(response, http.StatusCreated, created)
}

func (c *WorkController) Update(response http.ResponseWriter, request *http.Request) {
	var input updateWorkRequest
	if !decodeWorkRequest(response, request, &input) {
		return
	}
	updated, err := c.app.Update(request.Context(), work.UpdateInput{
		ID: request.PathValue("workID"), Title: input.Title, Description: input.Description,
		FolderID: input.FolderID, Status: input.Status, ExpectedRevision: input.ExpectedRevision,
	})
	if err != nil {
		writeAppError(response, c.logger, "update work", err)
		return
	}
	writeJSON(response, http.StatusOK, updated)
}

func (c *WorkController) Delete(response http.ResponseWriter, request *http.Request) {
	err := c.app.Delete(request.Context(), request.PathValue("workID"))
	if err != nil {
		writeAppError(response, c.logger, "delete work", err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (c *WorkController) ListFolders(response http.ResponseWriter, request *http.Request) {
	folders, err := c.app.ListFolders(request.Context())
	if err != nil {
		writeAppError(response, c.logger, "list work folders", err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"folders": folders})
}

func (c *WorkController) CreateFolder(response http.ResponseWriter, request *http.Request) {
	var input createWorkFolderRequest
	if !decodeWorkRequest(response, request, &input) {
		return
	}
	folder, err := c.app.CreateFolder(request.Context(), input.Name)
	if err != nil {
		writeAppError(response, c.logger, "create work folder", err)
		return
	}
	writeJSON(response, http.StatusCreated, folder)
}

func decodeWorkRequest(response http.ResponseWriter, request *http.Request, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(response, request.Body, maxWorkRequestBody))
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
