package internal

import "time"

// ======== API Request/Response Types ========

// RunRequest is the JSON body for POST /run
type RunRequest struct {
	Language         string      `json:"language"`
	Source           string      `json:"source"`
	SourceFile       string      `json:"source_filename,omitempty"`
	ArtifactFilename string      `json:"artifact_filename,omitempty"`
	Tests            []TestInput `json:"tests"`

	Build *PhaseOverride `json:"build,omitempty"`
	Run   *PhaseOverride `json:"run,omitempty"`

	// Backward-compatible fields kept for the local Stage 1 examples/tests.
	BuildFlags  []string `json:"build_flags,omitempty"`
	RunFlags    []string `json:"run_flags,omitempty"`
	BuildLimits *Limits  `json:"build_limits,omitempty"`
	RunLimits   *Limits  `json:"run_limits,omitempty"`
}

// TestInput represents a single test case
type TestInput struct {
	Stdin          string `json:"stdin"`
	ExpectedStdout string `json:"expected_stdout"`
	ExpectedOutput string `json:"expected_output"`
}

// PhaseOverride contains per-request build/run overrides.
type PhaseOverride struct {
	Limits *Limits  `json:"limits,omitempty"`
	Flags  []string `json:"flags,omitempty"`
}

// Expected returns the expected stdout using the brief's field first.
func (t TestInput) Expected() string {
	if t.ExpectedStdout != "" {
		return t.ExpectedStdout
	}
	return t.ExpectedOutput
}

// BuildFlagList returns build flags from the brief-shaped request first.
func (r RunRequest) BuildFlagList() []string {
	if r.Build != nil {
		return r.Build.Flags
	}
	return r.BuildFlags
}

// RunFlagList returns run flags from the brief-shaped request first.
func (r RunRequest) RunFlagList() []string {
	if r.Run != nil {
		return r.Run.Flags
	}
	return r.RunFlags
}

// BuildLimitOverride returns build limits from either accepted request shape.
func (r RunRequest) BuildLimitOverride() *Limits {
	if r.Build != nil && r.Build.Limits != nil {
		return r.Build.Limits
	}
	return r.BuildLimits
}

// RunLimitOverride returns run limits from either accepted request shape.
func (r RunRequest) RunLimitOverride() *Limits {
	if r.Run != nil && r.Run.Limits != nil {
		return r.Run.Limits
	}
	return r.RunLimits
}

// RunResponse is the JSON body returned from POST /run
type RunResponse struct {
	Status string       `json:"status"`
	Build  BuildResult  `json:"build"`
	Tests  []TestResult `json:"tests"`
}

// BuildResult contains build phase information
type BuildResult struct {
	Status     string `json:"status"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	DurationMs int64  `json:"duration_ms"`
}

// TestResult contains per-test results
type TestResult struct {
	Status       string `json:"status"`
	Stdout       string `json:"stdout"`
	Stderr       string `json:"stderr"`
	DurationMs   int64  `json:"duration_ms"`
	MemoryPeakKb int64  `json:"memory_peak_kb"`
}

// ErrorResponse is returned on HTTP 400/500 errors
type ErrorResponse struct {
	Error ErrorDetail `json:"error"`
}

// ErrorDetail contains error code and message
type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// HealthzResponse is returned from GET /healthz
type HealthzResponse struct {
	Status string `json:"status"`
}

// ReadyzResponse is returned from GET /readyz
type ReadyzResponse struct {
	Status    string                       `json:"status"`
	Nsjail    ReadyzCheckResult            `json:"nsjail"`
	Languages map[string]ReadyzCheckResult `json:"languages"`
}

// ReadyzCheckResult is the per-component readiness status
type ReadyzCheckResult struct {
	Ok      bool   `json:"ok"`
	Version string `json:"version,omitempty"`
	Error   string `json:"error,omitempty"`
}

// InfoResponse is returned from GET /info
type InfoResponse struct {
	BuildInfo BuildInfo      `json:"build_info"`
	Nsjail    NsjailInfo     `json:"nsjail"`
	Languages []LanguageInfo `json:"languages"`
	Limits    LimitInfo      `json:"limits"`
	Stats     StatsInfo      `json:"stats"`
}

// BuildInfo contains version information
type BuildInfo struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	GoVersion string `json:"go_version"`
}

// NsjailInfo contains nsjail path and version
type NsjailInfo struct {
	Path    string `json:"path"`
	Version string `json:"version"`
}

// LanguageInfo describes a supported language
type LanguageInfo struct {
	Id               string `json:"id"`
	Name             string `json:"name"`
	Version          string `json:"version"`
	DefaultRunLimits Limits `json:"default_run_limits"`
}

// Limits defines resource constraints
type Limits struct {
	WallTimeS    int `json:"wall_time_s,omitempty"`
	MemoryKb     int `json:"memory_kb,omitempty"`
	MaxProcesses int `json:"max_processes,omitempty"`
}

// LimitInfo describes global API limits
type LimitInfo struct {
	MaxSourceBytes    int `json:"max_source_bytes"`
	MaxTests          int `json:"max_tests"`
	MaxConcurrentJobs int `json:"max_concurrent_jobs"`
}

// StatsInfo contains server statistics
type StatsInfo struct {
	InFlightJobs         int        `json:"in_flight_jobs"`
	JobsTotal            int64      `json:"jobs_total"`
	JobsFailedInternal   int64      `json:"jobs_failed_internal"`
	LastInternalErrorAt  *time.Time `json:"last_internal_error_at,omitempty"`
	DiskFreeJailDirBytes int64      `json:"disk_free_bytes_jail_dir"`
}

// ======== Status Constants ========

const (
	// Build statuses
	BuildOk            = "ok"
	BuildFailed        = "failed"
	BuildInternalError = "internal_error"

	// Test statuses
	TestAccepted                 = "accepted"
	TestWrongOutput              = "wrong_output"
	TestOutputWhitespaceMismatch = "output_whitespace_mismatch"
	TestTimeExceeded             = "time_exceeded"
	TestMemoryExceeded           = "memory_exceeded"
	TestRuntimeError             = "runtime_error"
	TestNotExecuted              = "not_executed"
	TestInternalError            = "internal_error"

	// Top-level statuses
	TopAccepted                 = "accepted"
	TopBuildFailed              = "build_failed"
	TopWrongOutput              = "wrong_output"
	TopOutputWhitespaceMismatch = "output_whitespace_mismatch"
	TopTimeExceeded             = "time_exceeded"
	TopMemoryExceeded           = "memory_exceeded"
	TopRuntimeError             = "runtime_error"
	TopInternalError            = "internal_error"
)

// ======== Error Codes ========

const (
	ErrInvalidJSON     = "invalid_json"
	ErrUnknownLanguage = "unknown_language"
	ErrOversizeBody    = "oversize_body"
	ErrInvalidFilename = "invalid_filename"
	ErrDisallowedFlag  = "disallowed_flag"
	ErrTooManyTests    = "too_many_tests"
	ErrInternalError   = "internal_error"
)
