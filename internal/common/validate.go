package common

import (
	"errors"

	"github.com/gin-gonic/gin"
	"github.com/go-playground/validator/v10"
)

// Validate is the shared validator instance for request struct tags
// (e.g. `validate:"required,email"`).
var Validate = validator.New()

// FormatValidationErrors turns validator's per-field errors into FieldError,
// suitable for ValidationError's Errors field.
func FormatValidationErrors(err error) []FieldError {
	var verrs validator.ValidationErrors
	if !errors.As(err, &verrs) {
		return nil
	}

	out := make([]FieldError, 0, len(verrs))
	for _, fe := range verrs {
		out = append(out, FieldError{Field: fe.Field(), Message: fe.Tag()})
	}
	return out
}

// ParseAndValidate binds the request JSON body into form and validates it
// against its `validate` struct tags, returning a RestError ready to hand to
// HandleError on failure.
func ParseAndValidate[T any](c *gin.Context, form *T) error {
	if err := c.ShouldBindJSON(form); err != nil {
		return BadRequestError("invalid json format")
	}

	if err := Validate.Struct(form); err != nil {
		return ValidationError("validation error", FormatValidationErrors(err))
	}

	return nil
}
