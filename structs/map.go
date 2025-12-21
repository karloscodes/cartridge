// Package structs provides utilities for converting structs to maps and composing data for rendering.
package structs

import (
"reflect"
"strings"
)

// Map converts a struct to a map using JSON field tags.
func Map(in interface{}) map[string]interface{} {
tag := "json"
out := make(map[string]interface{})

v := reflect.ValueOf(in)
if v.Kind() == reflect.Ptr {
v = v.Elem()
}

typ := v.Type()
for i := 0; i < v.NumField(); i++ {
fi := typ.Field(i)
if tagv := fi.Tag.Get(tag); tagv != "" {
fieldName := strings.Split(tagv, ",")[0]
if fieldName != "" && fieldName != "-" {
out[fieldName] = v.Field(i).Interface()
}
}
}
return out
}

// Compose merges multiple maps together, with later maps taking precedence.
func Compose(maps ...map[string]interface{}) map[string]interface{} {
result := make(map[string]interface{})

for _, m := range maps {
for key, value := range m {
result[key] = value
}
}

return result
}
