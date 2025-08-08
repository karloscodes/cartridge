package cartridge

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRenderTemplateOrJSON(t *testing.T) {
	// Test with test environment - should return JSON
	testApp := NewAPIOnly(WithEnvironment("test"))
	
	testApp.Get("/test-json", func(ctx *Context) error {
		data := map[string]interface{}{
			"message": "Hello World",
			"status":  "success",
		}
		return ctx.RenderTemplateOrJSON("some-template", data)
	})

	// Test JSON response in test environment
	req := httptest.NewRequest("GET", "/test-json", nil)
	resp, err := testApp.fiberApp.Test(req)
	if err != nil {
		t.Fatalf("Failed to test JSON response: %v", err)
	}

	// Check that response is JSON
	if resp.StatusCode != 200 {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	// Read and parse response body
	buf := new(bytes.Buffer)
	buf.ReadFrom(resp.Body)
	responseBody := buf.String()

	// Verify it's valid JSON and contains our data
	var jsonResponse map[string]interface{}
	if err := json.Unmarshal([]byte(responseBody), &jsonResponse); err != nil {
		t.Errorf("Response is not valid JSON: %v", err)
	}

	if jsonResponse["message"] != "Hello World" {
		t.Errorf("Expected message 'Hello World', got %v", jsonResponse["message"])
	}

	if jsonResponse["status"] != "success" {
		t.Errorf("Expected status 'success', got %v", jsonResponse["status"])
	}

	// Test with production environment - should try to render template
	prodApp := NewFullStack(WithEnvironment("production"))
	
	prodApp.Get("/test-template", func(ctx *Context) error {
		data := map[string]interface{}{
			"message": "Hello World",
			"status":  "success",
		}
		return ctx.RenderTemplateOrJSON("some-template", data)
	})

	// Test template response in production environment
	req = httptest.NewRequest("GET", "/test-template", nil)
	resp, err = prodApp.fiberApp.Test(req)
	if err != nil {
		t.Fatalf("Failed to test template response: %v", err)
	}

	// In production, without a template, it should return an error
	// This is expected behavior since we don't have the template loaded
	if resp.StatusCode == 200 {
		// If it's 200, it means template rendering somehow worked
		buf = new(bytes.Buffer)
		buf.ReadFrom(resp.Body)
		responseBody = buf.String()
		
		// It shouldn't be JSON in production mode
		if strings.Contains(responseBody, `{"message":"Hello World"`) {
			t.Error("Production mode should not return JSON, but it did")
		}
	}

	t.Log("RenderTemplateOrJSON test passed")
}

func TestRenderTemplateOrJSONDevelopmentMode(t *testing.T) {
	// Test with development environment - should try to render template like production
	devApp := NewFullStack(WithEnvironment("development"))
	
	devApp.Get("/test-dev", func(ctx *Context) error {
		data := map[string]interface{}{
			"message": "Hello Dev World",
			"status":  "success",
		}
		return ctx.RenderTemplateOrJSON("some-template", data)
	})

	// Test template response in development environment
	req := httptest.NewRequest("GET", "/test-dev", nil)
	resp, err := devApp.fiberApp.Test(req)
	if err != nil {
		t.Fatalf("Failed to test development template response: %v", err)
	}

	// In development, without a template, it should also try to render template (not JSON)
	if resp.StatusCode == 200 {
		buf := new(bytes.Buffer)
		buf.ReadFrom(resp.Body)
		responseBody := buf.String()
		
		// It shouldn't be JSON in development mode either
		if strings.Contains(responseBody, `{"message":"Hello Dev World"`) {
			t.Error("Development mode should not return JSON, but it did")
		}
	}

	t.Log("RenderTemplateOrJSON development mode test passed")
}