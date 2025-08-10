package cartridge

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

// ValidationErrors represents field-specific validation errors
type ValidationErrors map[string][]string

// HasErrors returns true if there are any validation errors
func (ve ValidationErrors) HasErrors() bool {
	return len(ve) > 0
}

// Error returns a string representation of all errors
func (ve ValidationErrors) Error() string {
	var errors []string
	for field, fieldErrors := range ve {
		for _, err := range fieldErrors {
			errors = append(errors, field+": "+err)
		}
	}
	return strings.Join(errors, "; ")
}

// Add adds an error for a specific field
func (ve ValidationErrors) Add(field, message string) {
	ve[field] = append(ve[field], message)
}

// GetFirst returns the first error for a field
func (ve ValidationErrors) GetFirst(field string) string {
	if errors, ok := ve[field]; ok && len(errors) > 0 {
		return errors[0]
	}
	return ""
}

// FillModel securely fills model fields from form data using fillable tags
func (ctx *Context) FillModel(model interface{}) error {
	modelValue := reflect.ValueOf(model)
	if modelValue.Kind() != reflect.Ptr || modelValue.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("model must be a pointer to struct")
	}

	structValue := modelValue.Elem()
	structType := structValue.Type()

	for i := 0; i < structValue.NumField(); i++ {
		field := structValue.Field(i)
		fieldType := structType.Field(i)

		// Check if field is fillable
		fillableTag := fieldType.Tag.Get("fillable")
		if fillableTag != "true" {
			continue
		}

		// Get form name
		formName := fieldType.Tag.Get("form")
		if formName == "" {
			formName = strings.ToLower(fieldType.Name)
		}

		// Get form value
		formValue := ctx.FormValue(formName)
		if formValue == "" {
			continue
		}

		// Set field value with type conversion
		if err := setFieldValue(field, formValue); err != nil {
			ctx.Logger.Warn("Failed to set field value", "field", fieldType.Name, "error", err)
		}
	}

	return nil
}

// ValidateForm validates a struct using validation tags
func (ctx *Context) ValidateForm(model interface{}) ValidationErrors {
	errors := make(ValidationErrors)
	
	modelValue := reflect.ValueOf(model)
	if modelValue.Kind() == reflect.Ptr {
		modelValue = modelValue.Elem()
	}
	
	if modelValue.Kind() != reflect.Struct {
		errors.Add("general", "Invalid model type")
		return errors
	}

	structType := modelValue.Type()

	for i := 0; i < modelValue.NumField(); i++ {
		field := modelValue.Field(i)
		fieldType := structType.Field(i)

		validateTag := fieldType.Tag.Get("validate")
		if validateTag == "" {
			continue
		}

		fieldName := fieldType.Tag.Get("form")
		if fieldName == "" {
			fieldName = strings.ToLower(fieldType.Name)
		}

		// Parse validation rules
		rules := strings.Split(validateTag, ",")
		for _, rule := range rules {
			if err := validateRule(field, rule, fieldName); err != nil {
				label := fieldType.Tag.Get("label")
				if label == "" {
					label = fieldType.Name
				}
				errors.Add(fieldName, fmt.Sprintf("%s %s", label, err.Error()))
			}
		}
	}

	return errors
}

// Helper function to set field values with type conversion
func setFieldValue(field reflect.Value, value string) error {
	if !field.CanSet() {
		return fmt.Errorf("field cannot be set")
	}

	switch field.Kind() {
	case reflect.String:
		field.SetString(value)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if intVal, err := strconv.ParseInt(value, 10, 64); err == nil {
			field.SetInt(intVal)
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if uintVal, err := strconv.ParseUint(value, 10, 64); err == nil {
			field.SetUint(uintVal)
		}
	case reflect.Float32, reflect.Float64:
		if floatVal, err := strconv.ParseFloat(value, 64); err == nil {
			field.SetFloat(floatVal)
		}
	case reflect.Bool:
		field.SetBool(value == "true" || value == "1" || value == "on")
	}

	return nil
}

// Helper function to validate individual rules
func validateRule(field reflect.Value, rule string, fieldName string) error {
	rule = strings.TrimSpace(rule)
	
	if rule == "required" {
		if field.Kind() == reflect.String && field.String() == "" {
			return fmt.Errorf("is required")
		}
		if field.Kind() >= reflect.Int && field.Kind() <= reflect.Int64 && field.Int() == 0 {
			return fmt.Errorf("is required")
		}
		if field.Kind() >= reflect.Uint && field.Kind() <= reflect.Uint64 && field.Uint() == 0 {
			return fmt.Errorf("is required")
		}
	}

	if strings.HasPrefix(rule, "min=") {
		minStr := strings.TrimPrefix(rule, "min=")
		if min, err := strconv.Atoi(minStr); err == nil {
			if field.Kind() == reflect.String && len(field.String()) < min {
				return fmt.Errorf("must be at least %d characters", min)
			}
			if field.Kind() >= reflect.Int && field.Kind() <= reflect.Int64 && field.Int() < int64(min) {
				return fmt.Errorf("must be at least %d", min)
			}
		}
	}

	if strings.HasPrefix(rule, "max=") {
		maxStr := strings.TrimPrefix(rule, "max=")
		if max, err := strconv.Atoi(maxStr); err == nil {
			if field.Kind() == reflect.String && len(field.String()) > max {
				return fmt.Errorf("must be at most %d characters", max)
			}
			if field.Kind() >= reflect.Int && field.Kind() <= reflect.Int64 && field.Int() > int64(max) {
				return fmt.Errorf("must be at most %d", max)
			}
		}
	}

	return nil
}