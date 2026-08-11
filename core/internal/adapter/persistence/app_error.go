package persistence

import "warmmo/core/internal/application/apperror"

func databaseError(operation string, err error) error {
	return apperror.DatabaseError(operation, err)
}
