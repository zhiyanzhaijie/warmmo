package application

import (
	"errors"

	appagent "warmmo/core/internal/application/agent"
	"warmmo/core/internal/application/apperror"
	"warmmo/core/internal/application/pagination"
	"warmmo/core/internal/domain/ai"
	"warmmo/core/internal/domain/canvas"
	"warmmo/core/internal/domain/work"
)

type ErrorKind = apperror.Kind

const (
	ErrorInvalid  = apperror.Invalid
	ErrorNotFound = apperror.NotFound
	ErrorConflict = apperror.Conflict
	ErrorLocked   = apperror.Locked
	ErrorDatabase = apperror.Database
	ErrorUpstream = apperror.Upstream
	ErrorInternal = apperror.Internal
)

type AppError = apperror.Error

func InvalidError(code, message string, cause error) *AppError {
	return newAppError(ErrorInvalid, code, message, "validate application request", cause)
}

func NotFoundError(code, message string, cause error) *AppError {
	return newAppError(ErrorNotFound, code, message, "find application resource", cause)
}

func ConflictError(code, message string, cause error) *AppError {
	return newAppError(ErrorConflict, code, message, "resolve application conflict", cause)
}

func LockedError(code, message string, cause error) *AppError {
	return newAppError(ErrorLocked, code, message, "modify locked application resource", cause)
}

func DatabaseError(operation string, cause error) *AppError {
	return apperror.DatabaseError(operation, cause)
}

func UpstreamError(operation string, cause error) *AppError {
	return newAppError(ErrorUpstream, "UPSTREAM_ERROR", "上游服务暂时不可用", operation, cause)
}

func InternalError(operation string, cause error) *AppError {
	return newAppError(ErrorInternal, "INTERNAL_ERROR", "服务暂时不可用", operation, cause)
}

func ToAppError(operation string, err error) *AppError {
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr
	}

	switch {
	case errors.Is(err, work.ErrInvalidWork):
		return InvalidError("INVALID_WORK", "作品信息无效", err)
	case errors.Is(err, work.ErrNotFound):
		return NotFoundError("WORK_NOT_FOUND", "作品不存在", err)
	case errors.Is(err, work.ErrRevisionConflict):
		return ConflictError("WORK_REVISION_CONFLICT", "作品信息已更新，请重新加载后再操作", err)
	case errors.Is(err, work.ErrActiveRun):
		return ConflictError("WORK_HAS_ACTIVE_RUN", "作品仍有正在运行的 AI 任务，请稍后再操作", err)
	case errors.Is(err, work.ErrFolderNotFound):
		return NotFoundError("WORK_FOLDER_NOT_FOUND", "作品分类不存在", err)
	case errors.Is(err, work.ErrFolderConflict):
		return ConflictError("WORK_FOLDER_CONFLICT", "同名作品分类已经存在", err)
	case errors.Is(err, canvas.ErrNodeNotFound):
		return NotFoundError("CANVAS_NODE_NOT_FOUND", "画布节点不存在", err)
	case errors.Is(err, canvas.ErrInvalidNode), errors.Is(err, ErrInvalidCanvasRequest):
		return InvalidError("INVALID_CANVAS_REQUEST", "画布请求无效", err)
	case errors.Is(err, canvas.ErrRevisionConflict):
		return ConflictError("CANVAS_REVISION_CONFLICT", "节点已被更新，请重新加载后再操作", err)
	case errors.Is(err, canvas.ErrHistoryUnavailable):
		return ConflictError("CANVAS_HISTORY_UNAVAILABLE", "没有可执行的历史操作", err)
	case errors.Is(err, canvas.ErrCandidateNotFound):
		return NotFoundError("CANVAS_CANDIDATE_NOT_FOUND", "Candidate 不存在", err)
	case errors.Is(err, canvas.ErrCandidateResolved):
		return ConflictError("CANVAS_CANDIDATE_RESOLVED", "Candidate 已经处理", err)
	case errors.Is(err, canvas.ErrDerivationExists):
		return ConflictError("CANVAS_DERIVATION_EXISTS", "当前节点已经生成过派生节点", err)
	case errors.Is(err, canvas.ErrInvalidChapterArchive):
		return ConflictError("INVALID_CHAPTER_ARCHIVE", "章节归档状态无效", err)
	case errors.Is(err, canvas.ErrInvalidSectionOutline):
		return InvalidError("INVALID_SECTION_OUTLINE", "章节小节结构无效", err)
	case errors.Is(err, canvas.ErrChapterArchiveIncomplete):
		return ConflictError("CHAPTER_ARCHIVE_INCOMPLETE", "章节仍有未完成的小节", err)
	case errors.Is(err, canvas.ErrChapterArchiveNotFound):
		return NotFoundError("CHAPTER_ARCHIVE_NOT_FOUND", "章节归档不存在", err)
	case errors.Is(err, canvas.ErrChapterArchiveNotCurrent):
		return ConflictError("CHAPTER_ARCHIVE_NOT_CURRENT", "只能撤销当前章节归档", err)
	case errors.Is(err, canvas.ErrArchivedNodeLocked):
		return LockedError("CANVAS_NODE_ARCHIVED", "节点所属章节已归档，当前不可修改", err)
	case errors.Is(err, ErrInvalidProviderConfiguration), errors.Is(err, ai.ErrAPIKeyRequired):
		return InvalidError("INVALID_PROVIDER_CONFIGURATION", "Provider 配置无效", err)
	case errors.Is(err, ErrProviderNotFound):
		return NotFoundError("PROVIDER_NOT_FOUND", "Provider 不存在", err)
	case errors.Is(err, ErrProviderConfigurationNotFound), errors.Is(err, ai.ErrProviderConfigurationNotFound):
		return NotFoundError("PROVIDER_CONFIGURATION_NOT_FOUND", "Provider 尚未配置", err)
	case errors.Is(err, ErrInvalidAgentRun):
		return InvalidError("INVALID_AGENT_REQUEST", "Agent 请求无效", err)
	case errors.Is(err, appagent.ErrRunNotFound):
		return NotFoundError("AGENT_RUN_NOT_FOUND", "Agent Run 不存在", err)
	case errors.Is(err, appagent.ErrRunNotCancellable):
		return ConflictError("AGENT_RUN_NOT_CANCELLABLE", "Agent Run 当前无法取消", err)
	case errors.Is(err, appagent.ErrRunNotWaitingInput):
		return ConflictError("AGENT_RUN_NOT_WAITING_INPUT", "Agent Run 当前不等待输入", err)
	case errors.Is(err, appagent.ErrInvalidUserResponse):
		return ConflictError("INVALID_AGENT_RESPONSE", "这个问题已经回答或不再有效", err)
	case errors.Is(err, pagination.ErrInvalid):
		return InvalidError("INVALID_PAGINATION", "分页参数无效", err)
	default:
		return InternalError(operation, err)
	}
}

func newAppError(kind ErrorKind, code, message, operation string, cause error) *AppError {
	return apperror.New(kind, code, message, operation, cause)
}
