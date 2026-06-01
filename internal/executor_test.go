package internal

import (
	"testing"
)

// TestLanguageRegistry tests the language registry
func TestLanguageRegistry(t *testing.T) {
	registry := NewLanguageRegistry()

	// Create a test language
	lang := &Language{
		Id:   "test",
		Name: "Test Language",
	}

	if err := registry.Register(lang); err != nil {
		t.Fatalf("Failed to register language: %v", err)
	}

	// Get language
	retrieved, ok := registry.Get("test")
	if !ok {
		t.Fatal("Failed to retrieve registered language")
	}

	if retrieved.Id != lang.Id {
		t.Errorf("Retrieved language ID mismatch: got %s, want %s", retrieved.Id, lang.Id)
	}

	// Test All()
	all := registry.All()
	if len(all) != 1 {
		t.Errorf("Expected 1 language, got %d", len(all))
	}

	// Test AllIds()
	ids := registry.AllIds()
	if len(ids) != 1 || ids[0] != "test" {
		t.Errorf("AllIds failed: got %v", ids)
	}
}

// TestFilenameValidation tests path traversal protection
func TestFilenameValidation(t *testing.T) {
	testCases := []struct {
		name      string
		filename  string
		shouldErr bool
	}{
		{"simple", "solution.py", false},
		{"with extension", "test.cpp", false},
		{"path traversal", "../etc/passwd", true},
		{"absolute path", "/etc/passwd", true},
		{"with slash", "foo/bar.py", true},
		{"double dots", "foo..bar", false}, // dots in middle are OK
		{"empty", "", true},
		{"dot", ".", true},
	}

	tmpDir := t.TempDir()

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := WriteSourceFile(tmpDir, tc.filename, "test")
			if (err != nil) != tc.shouldErr {
				t.Errorf("WriteSourceFile(%q): got error=%v, want error=%v", tc.filename, err, tc.shouldErr)
			}
		})
	}
}

// TestFlagValidation tests flag allowlist enforcement
func TestFlagValidation(t *testing.T) {
	cpp := &Language{
		Id: "cpp",
		FlagAllowlist: map[string]bool{
			"-O0":     true,
			"-O1":     true,
			"-O2":     true,
			"-O3":     true,
			"-Wall":   true,
			"-Wextra": true,
			"-std=*":  true,
		},
	}

	testCases := []struct {
		name      string
		flags     []string
		shouldErr bool
	}{
		{"no flags", []string{}, false},
		{"single allowed", []string{"-O2"}, false},
		{"multiple allowed", []string{"-O2", "-Wall"}, false},
		{"wildcard match", []string{"-std=c++11"}, false},
		{"wildcard match 2", []string{"-std=c++20"}, false},
		{"disallowed flag", []string{"-fplugin=evil.so"}, true},
		{"mixed allowed and disallowed", []string{"-O2", "-fplugin=evil.so"}, true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := cpp.ValidateFlags(tc.flags)
			if (err != nil) != tc.shouldErr {
				t.Errorf("ValidateFlags(%v): got error=%v, want error=%v", tc.flags, err, tc.shouldErr)
			}
		})
	}
}

// TestDefaultRegistry tests that the default registry has expected languages
func TestDefaultRegistry(t *testing.T) {
	registry := DefaultRegistry()

	// Should have at least Python and C++
	langs := registry.AllIds()
	if len(langs) < 2 {
		t.Fatalf("Expected at least 2 languages, got %d", len(langs))
	}

	// Check Python
	py, ok := registry.Get("py3")
	if !ok {
		t.Fatal("Python 3 not registered")
	}
	if py.RunCmd == nil {
		t.Fatal("Python 3 run command not configured")
	}

	// Check C++
	cpp, ok := registry.Get("cpp")
	if !ok {
		t.Fatal("C++ not registered")
	}
	if cpp.BuildCmd == nil {
		t.Fatal("C++ build command not configured")
	}
	if cpp.RunCmd == nil {
		t.Fatal("C++ run command not configured")
	}
}

// TestStatusMapping tests correct status assignment
func TestStatusMapping(t *testing.T) {
	testCases := []struct {
		name         string
		buildStatus  string
		testStatuses []string
		expectedTop  string
	}{
		{
			"all accepted",
			BuildOk,
			[]string{TestAccepted, TestAccepted},
			TopAccepted,
		},
		{
			"build failed",
			BuildFailed,
			[]string{TestNotExecuted, TestNotExecuted},
			TopBuildFailed,
		},
		{
			"wrong output",
			BuildOk,
			[]string{TestWrongOutput, TestAccepted},
			TopWrongOutput,
		},
		{
			"whitespace mismatch",
			BuildOk,
			[]string{TestOutputWhitespaceMismatch},
			TopOutputWhitespaceMismatch,
		},
		{
			"time exceeded",
			BuildOk,
			[]string{TestTimeExceeded},
			TopTimeExceeded,
		},
		{
			"memory exceeded",
			BuildOk,
			[]string{TestMemoryExceeded},
			TopMemoryExceeded,
		},
		{
			"runtime error",
			BuildOk,
			[]string{TestRuntimeError},
			TopRuntimeError,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			build := BuildResult{Status: tc.buildStatus}
			tests := make([]TestResult, len(tc.testStatuses))
			for i, status := range tc.testStatuses {
				tests[i].Status = status
			}

			result := computeTopLevelStatus(build, tests)
			if result != tc.expectedTop {
				t.Errorf("computeTopLevelStatus: got %s, want %s", result, tc.expectedTop)
			}
		})
	}
}

// TestOutputComparison tests output comparison logic
func TestOutputComparison(t *testing.T) {
	testCases := []struct {
		name     string
		actual   string
		expected string
		status   string
	}{
		{"exact match", "hello\n", "hello\n", TestAccepted},
		{"whitespace trimmed", "hello", "hello\n", TestAccepted},
		{"trailing spaces", "hello   ", "hello", TestAccepted},
		{"whitespace only diff", "hello  world", "hello world", TestOutputWhitespaceMismatch},
		{"newline variation", "hello\nworld", "hello world", TestOutputWhitespaceMismatch},
		{"different output", "hello", "goodbye", TestWrongOutput},
		{"empty vs something", "", "hello", TestWrongOutput},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := compareOutput(tc.actual, tc.expected)
			if result != tc.status {
				t.Errorf("compareOutput: got %s, want %s", result, tc.status)
			}
		})
	}
}

// TestLimitValidation tests request limit validation
func TestLimitValidation(t *testing.T) {
	limits := &LimitInfo{
		MaxSourceBytes: 256,
		MaxTests:       5,
	}

	registry := NewLanguageRegistry()
	runner := NewSandboxRunner("/usr/bin/nsjail", "/tmp")
	executor := NewExecutor(registry, runner, limits)

	lang := &Language{
		Id: "test",
		RunCmd: &LanguageCmd{
			Cmd:    "python3",
			Args:   []string{"-c", "print('b')"},
			Limits: Limits{WallTimeS: 1, MemoryKb: 1000},
		},
	}
	registry.Register(lang)

	testCases := []struct {
		name       string
		req        *RunRequest
		shouldFail bool
	}{
		{
			"valid",
			&RunRequest{
				Language: "test",
				Source:   "test code",
				Tests:    []TestInput{{Stdin: "a", ExpectedOutput: "b"}},
			},
			false,
		},
		{
			"source too large",
			&RunRequest{
				Language: "test",
				Source:   string(make([]byte, limits.MaxSourceBytes+1)),
				Tests:    []TestInput{{Stdin: "a", ExpectedOutput: "b"}},
			},
			true,
		},
		{
			"too many tests",
			&RunRequest{
				Language: "test",
				Source:   "test code",
				Tests:    make([]TestInput, limits.MaxTests+1),
			},
			true,
		},
		{
			"no tests",
			&RunRequest{
				Language: "test",
				Source:   "test code",
				Tests:    []TestInput{},
			},
			true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			resp := executor.Execute(tc.req)
			hasFailed := resp.Status != TopAccepted && resp.Status != TopBuildFailed && resp.Status != TopWrongOutput
			if hasFailed != tc.shouldFail {
				t.Errorf("Expected failure=%v, got status=%s", tc.shouldFail, resp.Status)
			}
		})
	}
}
