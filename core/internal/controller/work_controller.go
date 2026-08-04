package controller

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"warmnote/core/internal/service"
	"warmnote/core/internal/work"
)

const maxWorkRequestBody = 16 * 1024

type WorkController struct {
	service *service.WorkService
	logger  *slog.Logger
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

func NewWorkController(workService *service.WorkService, logger *slog.Logger) *WorkController {
	return &WorkController{service: workService, logger: logger}
}

func (c *WorkController) List(response http.ResponseWriter, request *http.Request) {
	works, err := c.service.List(request.Context())
	if err != nil {
		c.internalError(response, "list works", err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"works": works})
}

func (c *WorkController) Get(response http.ResponseWriter, request *http.Request) {
	detail, err := c.service.Get(request.Context(), request.PathValue("workID"))
	switch {
	case errors.Is(err, work.ErrInvalidWork):
		writeJSON(response, http.StatusBadRequest, map[string]string{"message": "作品 ID 不能为空"})
	case errors.Is(err, work.ErrNotFound):
		writeJSON(response, http.StatusNotFound, map[string]string{"message": "作品不存在"})
	case err != nil:
		c.internalError(response, "get work", err)
	default:
		writeJSON(response, http.StatusOK, detail)
	}
}

func (c *WorkController) Create(response http.ResponseWriter, request *http.Request) {
	var input createWorkRequest
	decoder := json.NewDecoder(http.MaxBytesReader(response, request.Body, maxWorkRequestBody))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil {
		writeJSON(response, http.StatusBadRequest, map[string]string{"message": "请求内容无效"})
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeJSON(response, http.StatusBadRequest, map[string]string{"message": "请求只能包含一个 JSON 对象"})
		return
	}

	created, err := c.service.Create(request.Context(), work.CreateInput{
		Title: input.Title, Description: input.Description, FolderID: input.FolderID,
	})
	if errors.Is(err, work.ErrInvalidWork) {
		writeJSON(response, http.StatusBadRequest, map[string]string{"message": "作品名称或简介无效"})
		return
	}
	if errors.Is(err, work.ErrFolderNotFound) {
		writeJSON(response, http.StatusBadRequest, map[string]string{"message": "所选分类不存在"})
		return
	}
	if err != nil {
		c.internalError(response, "create work", err)
		return
	}
	writeJSON(response, http.StatusCreated, created)
}

func (c *WorkController) Update(response http.ResponseWriter, request *http.Request) {
	var input updateWorkRequest
	if !decodeWorkRequest(response, request, &input) {
		return
	}
	updated, err := c.service.Update(request.Context(), work.UpdateInput{
		ID: request.PathValue("workID"), Title: input.Title, Description: input.Description,
		FolderID: input.FolderID, Status: input.Status, ExpectedRevision: input.ExpectedRevision,
	})
	switch {
	case errors.Is(err, work.ErrInvalidWork):
		writeJSON(response, http.StatusBadRequest, map[string]string{"message": "作品信息无效"})
	case errors.Is(err, work.ErrFolderNotFound):
		writeJSON(response, http.StatusBadRequest, map[string]string{"message": "所选分类不存在"})
	case errors.Is(err, work.ErrNotFound):
		writeJSON(response, http.StatusNotFound, map[string]string{"message": "作品不存在"})
	case errors.Is(err, work.ErrRevisionConflict):
		writeJSON(response, http.StatusConflict, map[string]string{"message": "作品信息已更新，请重新打开后再保存"})
	case err != nil:
		c.internalError(response, "update work", err)
	default:
		writeJSON(response, http.StatusOK, updated)
	}
}

func (c *WorkController) ListFolders(response http.ResponseWriter, request *http.Request) {
	folders, err := c.service.ListFolders(request.Context())
	if err != nil {
		c.internalError(response, "list work folders", err)
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{"folders": folders})
}

func (c *WorkController) CreateFolder(response http.ResponseWriter, request *http.Request) {
	var input createWorkFolderRequest
	if !decodeWorkRequest(response, request, &input) {
		return
	}
	folder, err := c.service.CreateFolder(request.Context(), input.Name)
	if errors.Is(err, work.ErrInvalidWork) {
		writeJSON(response, http.StatusBadRequest, map[string]string{"message": "分类名称不能为空且不能超过 60 个字符"})
		return
	}
	if errors.Is(err, work.ErrFolderConflict) {
		writeJSON(response, http.StatusConflict, map[string]string{"message": "同名分类已经存在"})
		return
	}
	if err != nil {
		c.internalError(response, "create work folder", err)
		return
	}
	writeJSON(response, http.StatusCreated, folder)
}

func decodeWorkRequest(response http.ResponseWriter, request *http.Request, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(response, request.Body, maxWorkRequestBody))
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

func (c *WorkController) internalError(response http.ResponseWriter, operation string, err error) {
	c.logger.Error(operation, "error", err)
	writeJSON(response, http.StatusInternalServerError, map[string]string{"message": "作品服务不可用"})
}
