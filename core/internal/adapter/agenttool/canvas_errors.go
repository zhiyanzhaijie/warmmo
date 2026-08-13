package agenttool

import (
	"errors"

	appharness "warmmo/core/internal/application/harness"
	"warmmo/core/internal/domain/canvas"
)

func canvasToolError(err error) error {
	switch {
	case errors.Is(err, canvas.ErrNodeNotFound), errors.Is(err, canvas.ErrCandidateNotFound),
		errors.Is(err, canvas.ErrChapterArchiveNotFound):
		return appharness.NewToolError(appharness.ToolErrorNotFound, false, err)
	case errors.Is(err, canvas.ErrRevisionConflict), errors.Is(err, canvas.ErrCandidateResolved),
		errors.Is(err, canvas.ErrDerivationExists), errors.Is(err, canvas.ErrArchivedNodeLocked):
		return appharness.NewToolError(appharness.ToolErrorConflict, false, err)
	case errors.Is(err, canvas.ErrInvalidNode), errors.Is(err, canvas.ErrInvalidChapterArchive),
		errors.Is(err, canvas.ErrInvalidSectionOutline):
		return appharness.NewToolError(appharness.ToolErrorInvalidArgument, false, err)
	default:
		return err
	}
}
