//go:build integration
// +build integration

package tests

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"

	"github.com/thesouldev/goboxd/internal"
)

// TestHealthz tests the health check endpoint
func TestHealthz(t *testing.T) {
	resp, err := http.Get("http://localhost:8080/healthz")
	if err != nil {
		t.Fatalf("Failed to reach /healthz: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200, got %d", resp.StatusCode)
	}

	var result internal.HealthzResponse
	json.NewDecoder(resp.Body).Decode(&result)

	if result.Status != "ok" {
		t.Errorf("Expected status 'ok', got %q", result.Status)
	}
}

// TestReadyz tests the readiness endpoint
func TestReadyz(t *testing.T) {
	resp, err := http.Get("http://localhost:8080/readyz")
	if err != nil {
		t.Fatalf("Failed to reach /readyz: %v", err)
	}
	defer resp.Body.Close()

	var result internal.ReadyzResponse
	json.NewDecoder(resp.Body).Decode(&result)

	// May be ok or degraded depending on environment
	if result.Status != "ok" && result.Status != "degraded" {
		t.Errorf("Unexpected status: %q", result.Status)
	}

	if len(result.Languages) == 0 {
		t.Error("Expected languages in readyz response")
	}
}

// TestInfo tests the info endpoint
func TestInfo(t *testing.T) {
	resp, err := http.Get("http://localhost:8080/info")
	if err != nil {
		t.Fatalf("Failed to reach /info: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200, got %d", resp.StatusCode)
	}

	var result internal.InfoResponse
	json.NewDecoder(resp.Body).Decode(&result)

	if result.BuildInfo.Version == "" {
		t.Error("Expected version in build info")
	}

	if len(result.Languages) == 0 {
		t.Error("Expected languages in info response")
	}
}

// TestRunPython tests running Python code
func TestRunPython(t *testing.T) {
	req := internal.RunRequest{
		Language: "py3",
		Source:   "print('hello')",
		Tests: []internal.TestInput{
			{
				Stdin:          "",
				ExpectedOutput: "hello",
			},
		},
	}

	body, _ := json.Marshal(req)
	resp, err := http.Post(
		"http://localhost:8080/run",
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		t.Fatalf("Failed to post to /run: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200, got %d", resp.StatusCode)
	}

	var result internal.RunResponse
	json.NewDecoder(resp.Body).Decode(&result)

	if result.Status != internal.TopAccepted {
		t.Errorf("Expected status 'accepted', got %q", result.Status)
	}

	if len(result.Tests) == 0 {
		t.Fatal("Expected tests in response")
	}

	if result.Tests[0].Status != internal.TestAccepted {
		t.Errorf("Expected test status 'accepted', got %q", result.Tests[0].Status)
	}
}

// TestRunCpp tests running C++ code
func TestRunCpp(t *testing.T) {
	req := internal.RunRequest{
		Language: "cpp",
		Source: `#include <iostream>
int main() {
  std::cout << "hello" << std::endl;
  return 0;
}`,
		Tests: []internal.TestInput{
			{
				Stdin:          "",
				ExpectedOutput: "hello",
			},
		},
	}

	body, _ := json.Marshal(req)
	resp, err := http.Post(
		"http://localhost:8080/run",
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		t.Fatalf("Failed to post to /run: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected 200, got %d", resp.StatusCode)
	}

	var result internal.RunResponse
	json.NewDecoder(resp.Body).Decode(&result)

	// C++ should compile (build ok)
	if result.Build.Status != internal.BuildOk {
		t.Errorf("Build failed: %s", result.Build.Stderr)
	}

	if result.Status != internal.TopAccepted {
		t.Errorf("Expected status 'accepted', got %q. Output: %s", result.Status, result.Tests[0].Stdout)
	}
}

// TestRunInvalidLanguage tests error handling for unknown language
func TestRunInvalidLanguage(t *testing.T) {
	req := internal.RunRequest{
		Language: "unknown",
		Source:   "test",
		Tests: []internal.TestInput{
			{Stdin: "", ExpectedOutput: ""},
		},
	}

	body, _ := json.Marshal(req)
	resp, err := http.Post(
		"http://localhost:8080/run",
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		t.Fatalf("Failed to post to /run: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("Expected 400, got %d", resp.StatusCode)
	}

	var errResp internal.ErrorResponse
	json.NewDecoder(resp.Body).Decode(&errResp)

	if errResp.Error.Code != internal.ErrUnknownLanguage {
		t.Errorf("Expected error code %q, got %q", internal.ErrUnknownLanguage, errResp.Error.Code)
	}
}

// TestRunInvalidFilename tests path traversal protection
func TestRunInvalidFilename(t *testing.T) {
	req := internal.RunRequest{
		Language:   "py3",
		Source:     "print('test')",
		SourceFile: "../../etc/passwd",
		Tests: []internal.TestInput{
			{Stdin: "", ExpectedOutput: ""},
		},
	}

	body, _ := json.Marshal(req)
	resp, err := http.Post(
		"http://localhost:8080/run",
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		t.Fatalf("Failed to post to /run: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("Expected 400, got %d", resp.StatusCode)
	}

	var errResp internal.ErrorResponse
	json.NewDecoder(resp.Body).Decode(&errResp)

	if errResp.Error.Code != internal.ErrInvalidFilename {
		t.Errorf("Expected error code %q, got %q", internal.ErrInvalidFilename, errResp.Error.Code)
	}
}

// TestRunWrongOutput tests output mismatch detection
func TestRunWrongOutput(t *testing.T) {
	req := internal.RunRequest{
		Language: "py3",
		Source:   "print('goodbye')",
		Tests: []internal.TestInput{
			{
				Stdin:          "",
				ExpectedOutput: "hello",
			},
		},
	}

	body, _ := json.Marshal(req)
	resp, err := http.Post(
		"http://localhost:8080/run",
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		t.Fatalf("Failed to post to /run: %v", err)
	}
	defer resp.Body.Close()

	var result internal.RunResponse
	json.NewDecoder(resp.Body).Decode(&result)

	if result.Status != internal.TopWrongOutput {
		t.Errorf("Expected status 'wrong_output', got %q", result.Status)
	}

	if result.Tests[0].Status != internal.TestWrongOutput {
		t.Errorf("Expected test status 'wrong_output', got %q", result.Tests[0].Status)
	}
}

// TestRunMultipleTests tests running multiple test cases
func TestRunMultipleTests(t *testing.T) {
	req := internal.RunRequest{
		Language: "py3",
		Source: `import sys
input_val = input()
print(int(input_val) * 2)`,
		Tests: []internal.TestInput{
			{Stdin: "5", ExpectedOutput: "10"},
			{Stdin: "3", ExpectedOutput: "6"},
			{Stdin: "0", ExpectedOutput: "0"},
		},
	}

	body, _ := json.Marshal(req)
	resp, err := http.Post(
		"http://localhost:8080/run",
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		t.Fatalf("Failed to post to /run: %v", err)
	}
	defer resp.Body.Close()

	var result internal.RunResponse
	json.NewDecoder(resp.Body).Decode(&result)

	if len(result.Tests) != 3 {
		t.Fatalf("Expected 3 tests, got %d", len(result.Tests))
	}

	for i, test := range result.Tests {
		if test.Status != internal.TestAccepted {
			t.Errorf("Test %d: expected 'accepted', got %q", i, test.Status)
		}
	}
}

// TestConcurrency simulates concurrent requests
func TestConcurrency(t *testing.T) {
	done := make(chan error, 5)

	for i := 0; i < 5; i++ {
		go func() {
			req := internal.RunRequest{
				Language: "py3",
				Source:   "print('ok')",
				Tests: []internal.TestInput{
					{Stdin: "", ExpectedOutput: "ok"},
				},
			}

			body, _ := json.Marshal(req)
			resp, err := http.Post(
				"http://localhost:8080/run",
				"application/json",
				bytes.NewReader(body),
			)
			if err != nil {
				done <- err
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				done <- io.EOF
				return
			}

			var result internal.RunResponse
			json.NewDecoder(resp.Body).Decode(&result)

			if result.Status != internal.TopAccepted {
				done <- io.EOF
				return
			}

			done <- nil
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 5; i++ {
		if err := <-done; err != nil {
			t.Errorf("Concurrent request failed: %v", err)
		}
	}
}

// Helper to wait for server startup
func WaitForServer(maxWait time.Duration) error {
	deadline := time.Now().Add(maxWait)
	for {
		if time.Now().After(deadline) {
			return io.EOF
		}

		resp, err := http.Get("http://localhost:8080/healthz")
		if err == nil {
			resp.Body.Close()
			return nil
		}

		time.Sleep(100 * time.Millisecond)
	}
}

// TestMain runs before tests to ensure server is ready
func init() {
	// Give server 30 seconds to start
	WaitForServer(30 * time.Second)
}
