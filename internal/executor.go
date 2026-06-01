package internal

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// Executor runs code and returns results matching the API contract
type Executor struct {
	Registry *LanguageRegistry
	Runner   *SandboxRunner
	Limits   *LimitInfo
}

// NewExecutor creates a new executor
func NewExecutor(registry *LanguageRegistry, runner *SandboxRunner, limits *LimitInfo) *Executor {
	return &Executor{
		Registry: registry,
		Runner:   runner,
		Limits:   limits,
	}
}

// Execute runs source code for a language with given tests
func (e *Executor) Execute(req *RunRequest) *RunResponse {
	resp := &RunResponse{
		Build: BuildResult{Status: BuildInternalError},
		Tests: make([]TestResult, 0),
	}

	lang, ok := e.Registry.Get(req.Language)
	if !ok {
		resp.Status = TopInternalError
		resp.Build.Stderr = "unknown language"
		return resp
	}

	buildFlags := requestBuildFlags(req)
	runFlags := requestRunFlags(req)

	if err := lang.ValidateFlags(buildFlags); err != nil {
		resp.Status = TopInternalError
		resp.Build.Stderr = err.Error()
		return resp
	}
	if err := lang.ValidateFlags(runFlags); err != nil {
		resp.Status = TopInternalError
		resp.Build.Stderr = err.Error()
		return resp
	}

	if len(req.Source) == 0 || len(req.Source) > e.Limits.MaxSourceBytes {
		resp.Status = TopInternalError
		resp.Build.Stderr = fmt.Sprintf("source must be 1-%d bytes", e.Limits.MaxSourceBytes)
		return resp
	}
	if len(req.Tests) == 0 || len(req.Tests) > e.Limits.MaxTests {
		resp.Status = TopInternalError
		resp.Build.Stderr = fmt.Sprintf("tests must be 1-%d", e.Limits.MaxTests)
		return resp
	}

	sourceFile := req.SourceFile
	if sourceFile == "" {
		sourceFile = lang.SourceFilename
	}
	if sourceFile == "" {
		sourceFile = "solution.txt"
	}

	jailPath, err := e.Runner.CreateJailDirectory()
	if err != nil {
		resp.Status = TopInternalError
		resp.Build.Stderr = "failed to create jail"
		return resp
	}
	defer e.Runner.CleanupJailDirectory(jailPath)

	_, err = WriteSourceFile(jailPath, sourceFile, req.Source)
	if err != nil {
		resp.Status = TopInternalError
		resp.Build.Stderr = fmt.Sprintf("invalid filename: %v", err)
		return resp
	}

	if lang.BuildCmd != nil {
		buildResp := e.runBuildPhase(lang, jailPath, sourceFile, req.ArtifactFilename, buildFlags, req.BuildLimitOverride())
		resp.Build = buildResp

		if buildResp.Status != BuildOk {
			resp.Status = TopBuildFailed
			for range req.Tests {
				resp.Tests = append(resp.Tests, TestResult{
					Status: TestNotExecuted,
				})
			}
			return resp
		}

		// Make artifact executable
		artifactPath := filepath.Join(jailPath, resolveArtifactName(lang, req.ArtifactFilename))
		if err := os.Chmod(artifactPath, 0755); err != nil {
			log.Printf("WARN: Could not chmod artifact %s: %v", artifactPath, err)
		}
	} else {
		resp.Build = BuildResult{
			Status:     BuildOk,
			Stdout:     "",
			Stderr:     "",
			DurationMs: 0,
		}
	}

	resp.Tests = e.runTestPhase(lang, jailPath, sourceFile, req.ArtifactFilename, req.Tests, runFlags, req.RunLimitOverride())
	resp.Status = computeTopLevelStatus(resp.Build, resp.Tests)

	return resp
}

// runBuildPhase executes the build command
func (e *Executor) runBuildPhase(lang *Language, jailPath string, sourceFile string, artifactFile string, flags []string, override *Limits) BuildResult {
	result := BuildResult{}

	artifact := resolveArtifactName(lang, artifactFile)
	cmd := expandOne(lang.BuildCmd.Cmd, sourceFile, artifact)
	args := e.expandArgs(lang.BuildCmd.Args, sourceFile, artifact, flags)
	limits := mergeLimits(lang.BuildCmd.Limits, override)

	stdout, stderr, duration, err := e.Runner.ExecuteCommand(cmd, args, limits, "", jailPath)
	result.DurationMs = duration
	result.Stdout = stdout
	result.Stderr = stderr

	// Debug logging – shows actual compiler output
	log.Printf("DEBUG: Build stdout:\n%s", stdout)
	log.Printf("DEBUG: Build stderr:\n%s", stderr)
	log.Printf("DEBUG: Build error (exit code): %v", err)

	if err != nil {
		result.Status = BuildFailed
	} else {
		result.Status = BuildOk
	}

	return result
}

// runTestPhase executes all test cases
func (e *Executor) runTestPhase(lang *Language, jailPath string, sourceFile string, artifactFile string, tests []TestInput, flags []string, override *Limits) []TestResult {
	results := make([]TestResult, len(tests))
	artifact := resolveArtifactName(lang, artifactFile)
	limits := mergeLimits(lang.RunCmd.Limits, override)

	for i, test := range tests {
		result := TestResult{}

		cmd := expandOne(lang.RunCmd.Cmd, sourceFile, artifact)
		args := e.expandArgs(lang.RunCmd.Args, sourceFile, artifact, flags)

		stdout, stderr, duration, err := e.Runner.ExecuteCommand(cmd, args, limits, test.Stdin, jailPath)
		result.DurationMs = duration
		result.Stdout = stdout
		result.Stderr = stderr

		if err != nil {
			result.Status = TestRuntimeError
		} else {
			result.Status = compareOutput(stdout, test.Expected())
		}

		result.MemoryPeakKb = 0
		results[i] = result
	}

	return results
}

func requestBuildFlags(req *RunRequest) []string {
	return req.BuildFlagList()
}

func requestRunFlags(req *RunRequest) []string {
	return req.RunFlagList()
}

func resolveArtifactName(lang *Language, requested string) string {
	if requested != "" {
		return requested
	}
	return lang.ArtifactName
}

func expandOne(value string, sourceFile string, artifactName string) string {
	value = strings.ReplaceAll(value, "{{source}}", sourceFile)
	value = strings.ReplaceAll(value, "{{artifact}}", artifactName)
	return value
}

func mergeLimits(defaults Limits, override *Limits) Limits {
	if override == nil {
		return defaults
	}
	merged := defaults
	if override.WallTimeS != 0 {
		merged.WallTimeS = override.WallTimeS
	}
	if override.MemoryKb != 0 {
		merged.MemoryKb = override.MemoryKb
	}
	if override.MaxProcesses != 0 {
		merged.MaxProcesses = override.MaxProcesses
	}
	return merged
}

// expandArgs replaces template placeholders in command arguments
func (e *Executor) expandArgs(args []string, sourceFile string, artifactName string, customFlags []string) []string {
	result := make([]string, 0, len(args)+len(customFlags))

	for _, arg := range args {
		switch arg {
		case "{{source}}":
			result = append(result, sourceFile)
		case "{{artifact}}":
			result = append(result, artifactName)
		case "{{flags}}":
			result = append(result, customFlags...)
		default:
			result = append(result, arg)
		}
	}

	return result
}

// compareOutput checks if output matches expected (with whitespace flexibility)
func compareOutput(actual, expected string) string {
	actualTrimmed := strings.TrimSpace(actual)
	expectedTrimmed := strings.TrimSpace(expected)

	if actualTrimmed == expectedTrimmed {
		return TestAccepted
	}

	if strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(actualTrimmed, " ", ""), "\n", ""), "\t", "") ==
		strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(expectedTrimmed, " ", ""), "\n", ""), "\t", "") {
		return TestOutputWhitespaceMismatch
	}

	return TestWrongOutput
}

// computeTopLevelStatus determines the final status from build and tests
func computeTopLevelStatus(build BuildResult, tests []TestResult) string {
	if build.Status != BuildOk {
		return TopBuildFailed
	}

	for _, test := range tests {
		if test.Status != TestAccepted {
			switch test.Status {
			case TestWrongOutput:
				return TopWrongOutput
			case TestOutputWhitespaceMismatch:
				return TopOutputWhitespaceMismatch
			case TestTimeExceeded:
				return TopTimeExceeded
			case TestMemoryExceeded:
				return TopMemoryExceeded
			case TestRuntimeError:
				return TopRuntimeError
			case TestInternalError:
				return TopInternalError
			default:
				return TopRuntimeError
			}
		}
	}

	return TopAccepted
}