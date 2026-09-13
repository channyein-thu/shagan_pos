package common

import "net/http"

// RestError is a typed HTTP error carrying its own status code. Return it
// (or a wrapped version of it) from repository/service methods so HandleError
// can pick the right status automatically - the handler never needs to guess.
type RestError struct {
	Status  int          `json:"-"`
	Message string       `json:"message"`
	Errors  []FieldError `json:"errors,omitempty"`
}

func (e RestError) Error() string {
	return e.Message
}

// FieldError is one field-level validation failure.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

func BadRequestError(message string) RestError {
	return RestError{Status: http.StatusBadRequest, Message: message}
}

func UnauthorizedError(message string) RestError {
	return RestError{Status: http.StatusUnauthorized, Message: message}
}

func ForbiddenError(message string) RestError {
	return RestError{Status: http.StatusForbidden, Message: message}
}

func NotFoundError(message string) RestError {
	return RestError{Status: http.StatusNotFound, Message: message}
}

func ConflictError(message string) RestError {
	return RestError{Status: http.StatusConflict, Message: message}
}

func SystemError(message string) RestError {
	return RestError{Status: http.StatusInternalServerError, Message: message}
}

func NotImplementedError(message string) RestError {
	return RestError{Status: http.StatusNotImplemented, Message: message}
}

func ValidationError(message string, errs []FieldError) RestError {
	return RestError{
		Status:  http.StatusBadRequest,
		Message: message,
		Errors:  errs,
	}
}

// ErrNotImplemented is returned by stubbed repository/service methods until
// their real logic is written. It's a RestError so HandleError already
// reports it as 501 with no extra handling needed at the call site.
var ErrNotImplemented = NotImplementedError("not implemented")
