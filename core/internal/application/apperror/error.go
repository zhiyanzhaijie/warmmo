package apperror

import "fmt"

type Kind string

const (
	Invalid  Kind = "invalid"
	NotFound Kind = "not_found"
	Conflict Kind = "conflict"
	Locked   Kind = "locked"
	Database Kind = "database"
	Upstream Kind = "upstream"
	Internal Kind = "internal"
)

type Error struct {
	kind          Kind
	code          string
	publicMessage string
	operation     string
	cause         error
}

func New(kind Kind, code, message, operation string, cause error) *Error {
	return &Error{
		kind:          kind,
		code:          code,
		publicMessage: message,
		operation:     operation,
		cause:         cause,
	}
}

func (e *Error) Error() string {
	if e.cause == nil {
		return e.operation
	}
	return fmt.Sprintf("%s: %v", e.operation, e.cause)
}

func (e *Error) Unwrap() error { return e.cause }

func (e *Error) Kind() Kind { return e.kind }

func (e *Error) Code() string { return e.code }

func (e *Error) PublicMessage() string { return e.publicMessage }

func DatabaseError(operation string, cause error) *Error {
	return New(Database, "DATABASE_ERROR", "数据服务暂时不可用", operation, cause)
}
