package cartridge

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/go-playground/validator/v10"
)

// Global validator instance - thread safe
var validate *validator.Validate

func init() {
	validate = validator.New()
	
	// Use JSON tag names in error messages instead of struct field names
	validate.RegisterTagNameFunc(func(fld reflect.StructField) string {
		name := strings.SplitN(fld.Tag.Get("json"), ",", 2)[0]
		if name == "-" {
			return ""
		}
		if name == "" {
			// Fallback to form tag
			name = strings.SplitN(fld.Tag.Get("form"), ",", 2)[0]
			if name == "-" {
				return ""
			}
		}
		return name
	})
}

// ValidateStruct validates a struct using validator tags
// Usage: if err := cartridge.ValidateStruct(user); err != nil { ... }
func ValidateStruct(s interface{}) error {
	return validate.Struct(s)
}

// ValidateStructWithMessage validates struct and returns formatted error message
func ValidateStructWithMessage(s interface{}) (bool, string) {
	if err := validate.Struct(s); err != nil {
		return false, FormatValidationError(err)
	}
	return true, ""
}

// FormatValidationError converts validator errors to readable messages
func FormatValidationError(err error) string {
	if validationErrors, ok := err.(validator.ValidationErrors); ok {
		var messages []string
		for _, fieldError := range validationErrors {
			messages = append(messages, formatFieldError(fieldError))
		}
		return strings.Join(messages, "; ")
	}
	return err.Error()
}

// formatFieldError creates human-readable error message for a field
func formatFieldError(fe validator.FieldError) string {
	field := fe.Field()
	if field == "" {
		field = fe.Tag() // Fallback to tag name
	}
	
	switch fe.Tag() {
	case "required":
		return fmt.Sprintf("%s is required", field)
	case "email":
		return fmt.Sprintf("%s must be a valid email", field)
	case "min":
		return fmt.Sprintf("%s must be at least %s characters", field, fe.Param())
	case "max":
		return fmt.Sprintf("%s must be at most %s characters", field, fe.Param())
	case "len":
		return fmt.Sprintf("%s must be exactly %s characters", field, fe.Param())
	case "gte":
		return fmt.Sprintf("%s must be greater than or equal to %s", field, fe.Param())
	case "lte":
		return fmt.Sprintf("%s must be less than or equal to %s", field, fe.Param())
	case "gt":
		return fmt.Sprintf("%s must be greater than %s", field, fe.Param())
	case "lt":
		return fmt.Sprintf("%s must be less than %s", field, fe.Param())
	case "oneof":
		return fmt.Sprintf("%s must be one of: %s", field, fe.Param())
	case "url":
		return fmt.Sprintf("%s must be a valid URL", field)
	case "numeric":
		return fmt.Sprintf("%s must be numeric", field)
	case "alpha":
		return fmt.Sprintf("%s must contain only letters", field)
	case "alphanum":
		return fmt.Sprintf("%s must contain only letters and numbers", field)
	case "uuid":
		return fmt.Sprintf("%s must be a valid UUID", field)
	default:
		return fmt.Sprintf("%s failed %s validation", field, fe.Tag())
	}
}

// Validator wraps go-playground/validator for chainable validation
type Validator struct {
	errors []string
}

// NewValidator creates a new chainable validator
func NewValidator() *Validator {
	return &Validator{errors: []string{}}
}

// Check adds a struct validation to the chain
func (v *Validator) Check(s interface{}) *Validator {
	if err := validate.Struct(s); err != nil {
		v.errors = append(v.errors, FormatValidationError(err))
	}
	return v
}

// CheckField adds a single field validation to the chain
func (v *Validator) CheckField(field interface{}, tag string, message ...string) *Validator {
	if err := validate.Var(field, tag); err != nil {
		if len(message) > 0 {
			v.errors = append(v.errors, message[0])
		} else {
			v.errors = append(v.errors, FormatValidationError(err))
		}
	}
	return v
}

// Custom validation function
func (v *Validator) CheckCustom(isValid bool, message string) *Validator {
	if !isValid {
		v.errors = append(v.errors, message)
	}
	return v
}

// IsValid returns true if no validation errors occurred
func (v *Validator) IsValid() bool {
	return len(v.errors) == 0
}

// Errors returns all validation error messages
func (v *Validator) Errors() []string {
	return v.errors
}

// ErrorMessage returns combined error message string
func (v *Validator) ErrorMessage() string {
	if len(v.errors) == 0 {
		return ""
	}
	return strings.Join(v.errors, "; ")
}

// Error returns a combined error or nil if valid
func (v *Validator) Error() error {
	if len(v.errors) == 0 {
		return nil
	}
	return fmt.Errorf(v.ErrorMessage())
}

// Quick validation helpers for common cases

// ValidateEmail validates email format
func ValidateEmail(email string) error {
	return validate.Var(email, "required,email")
}

// ValidateRequired validates field is not empty
func ValidateRequired(field interface{}) error {
	return validate.Var(field, "required")
}

// ValidateMinLength validates minimum string length
func ValidateMinLength(str string, min int) error {
	return validate.Var(str, fmt.Sprintf("min=%d", min))
}

// ValidateMaxLength validates maximum string length
func ValidateMaxLength(str string, max int) error {
	return validate.Var(str, fmt.Sprintf("max=%d", max))
}

// ValidateRange validates number is within range
func ValidateRange(val interface{}, min, max float64) error {
	return validate.Var(val, fmt.Sprintf("gte=%f,lte=%f", min, max))
}

// ValidateURL validates URL format
func ValidateURL(url string) error {
	return validate.Var(url, "url")
}

// ValidateOneOf validates value is one of allowed options
func ValidateOneOf(val string, options []string) error {
	return validate.Var(val, fmt.Sprintf("oneof=%s", strings.Join(options, " ")))
}