package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"sync"
	"time"

	"github.com/thesouldev/goboxd/internal"
)

var (
	// Build info (set at compile time)
	Version   = "0.1.0"
	Commit    = "dev"
	GoVersion = runtime.Version()

	// Global state
	registry   *internal.LanguageRegistry
	executor   *internal.Executor
	runner     *internal.SandboxRunner
	limitInfo  *internal.LimitInfo
	stats      *ServerStats
	nsjailPath string
)

// ServerStats tracks server statistics
type ServerStats struct {
	mu                  sync.RWMutex
	inFlightJobs        int
	jobsTotal           int64
	jobsFailedInternal  int64
	lastInternalErrorAt *time.Time
}

func (s *ServerStats) recordJob() {
	s.mu.Lock()
	s.inFlightJobs++
	s.jobsTotal++
	s.mu.Unlock()
}

func (s *ServerStats) completeJob() {
	s.mu.Lock()
	s.inFlightJobs--
	s.mu.Unlock()
}

func (s *ServerStats) recordError() {
	s.mu.Lock()
	s.jobsFailedInternal++
	now := time.Now()
	s.lastInternalErrorAt = &now
	s.mu.Unlock()
}

func (s *ServerStats) getStats() (int, int64, int64, *time.Time) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.inFlightJobs, s.jobsTotal, s.jobsFailedInternal, s.lastInternalErrorAt
}

func init() {
	// Initialize language registry
	registry = internal.DefaultRegistry()

	// Initialize limits
	limitInfo = &internal.LimitInfo{
		MaxSourceBytes:    262144, // 256 KB
		MaxTests:          50,
		MaxConcurrentJobs: runtime.NumCPU(),
	}
	if maxJobs := os.Getenv("MAX_JOBS"); maxJobs != "" {
		if parsed, err := strconv.Atoi(maxJobs); err == nil && parsed > 0 {
			limitInfo.MaxConcurrentJobs = parsed
		}
	}

	// Initialize sandbox runner
	jailDir := os.Getenv("JAIL_DIR")
	if jailDir == "" {
		jailDir = "/var/run/goboxd-jail"
	}

	// Find nsjail
	nsjailPath, _ = exec.LookPath("nsjail")
	if nsjailPath == "" {
		nsjailPath = "/usr/local/bin/nsjail"
	}
	// If still not found, try hardcoded path
	if _, err := os.Stat(nsjailPath); os.IsNotExist(err) {
		nsjailPath = "/usr/bin/nsjail"
	}

	runner = internal.NewSandboxRunner(nsjailPath, jailDir)

	// Initialize executor
	executor = internal.NewExecutor(registry, runner, limitInfo)

	// Initialize stats
	stats = &ServerStats{}
}

func main() {
	// Setup routes
	http.HandleFunc("/healthz", handleHealthz)
	http.HandleFunc("/readyz", handleReadyz)
	http.HandleFunc("/info", handleInfo)
	http.HandleFunc("/run", handleRun)

	// Start server
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	addr := fmt.Sprintf(":%s", port)
	log.Printf("goboxd starting on %s", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatal(err)
	}
}

// handleHealthz responds to liveness checks
func handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(internal.HealthzResponse{Status: "ok"})
}

// handleReadyz responds to readiness checks
func handleReadyz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	resp := internal.ReadyzResponse{
		Status:    "ok",
		Languages: make(map[string]internal.ReadyzCheckResult),
	}

	// Check nsjail
	if nsjailPath == "" {
		resp.Status = "degraded"
		resp.Nsjail = internal.ReadyzCheckResult{
			Ok:    false,
			Error: "nsjail not found",
		}
	} else {
		cmd := exec.Command(nsjailPath, "--help")
		out, err := cmd.Output()
		if err != nil {
			resp.Status = "degraded"
			resp.Nsjail = internal.ReadyzCheckResult{Ok: false, Error: "nsjail not executable"}
		} else {
			version := internal.NsjailVersionFromHelp(string(out))
			resp.Nsjail = internal.ReadyzCheckResult{Ok: true, Version: version}
		}
	}

	// Check languages
	for _, lang := range registry.All() {
		ok, version, err := internal.CheckLanguageAvailability(lang)
		if !ok {
			resp.Status = "degraded"
			resp.Languages[lang.Id] = internal.ReadyzCheckResult{
				Ok:    false,
				Error: err.Error(),
			}
		} else {
			resp.Languages[lang.Id] = internal.ReadyzCheckResult{
				Ok:      true,
				Version: version,
			}
		}
	}

	// Set response code
	statusCode := http.StatusOK
	if resp.Status != "ok" {
		statusCode = http.StatusServiceUnavailable
	}

	w.WriteHeader(statusCode)
	json.NewEncoder(w).Encode(resp)
}

// handleInfo returns system information
func handleInfo(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Get nsjail version
	var nsjailVersion string
	if nsjailPath != "" {
		cmd := exec.Command(nsjailPath, "--help")
		out, _ := cmd.Output()
		nsjailVersion = internal.NsjailVersionFromHelp(string(out))
	}

	// Build language list
	langInfos := make([]internal.LanguageInfo, 0)
	for _, lang := range registry.All() {
		_, version, _ := internal.CheckLanguageAvailability(lang)
		limits := lang.RunCmd.Limits
		if limits.WallTimeS == 0 {
			limits.WallTimeS = 10
		}
		if limits.MemoryKb == 0 {
			limits.MemoryKb = 102400
		}
		if limits.MaxProcesses == 0 {
			limits.MaxProcesses = 100
		}

		langInfos = append(langInfos, internal.LanguageInfo{
			Id:               lang.Id,
			Name:             lang.Name,
			Version:          version,
			DefaultRunLimits: limits,
		})
	}

	// Get disk free space
	var diskFree int64
	if _, err := os.Stat(runner.JailDir); err == nil {
		if err := os.MkdirAll(runner.JailDir, 0755); err == nil {
			diskFree = getDiskFree(runner.JailDir)
		}
	}

	inFlight, total, failed, lastErr := stats.getStats()

	resp := internal.InfoResponse{
		BuildInfo: internal.BuildInfo{
			Version:   Version,
			Commit:    Commit,
			GoVersion: GoVersion,
		},
		Nsjail: internal.NsjailInfo{
			Path:    nsjailPath,
			Version: nsjailVersion,
		},
		Languages: langInfos,
		Limits:    *limitInfo,
		Stats: internal.StatsInfo{
			InFlightJobs:         inFlight,
			JobsTotal:            total,
			JobsFailedInternal:   failed,
			LastInternalErrorAt:  lastErr,
			DiskFreeJailDirBytes: diskFree,
		},
	}

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

// handleRun executes source code
func handleRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	stats.recordJob()
	defer stats.completeJob()

	// Parse request
	// Limit body size
	r.Body = http.MaxBytesReader(w, r.Body, 10*1024*1024) // 10 MB

	var req internal.RunRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		stats.recordError()
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(internal.ErrorResponse{
			Error: internal.ErrorDetail{
				Code:    internal.ErrInvalidJSON,
				Message: "invalid JSON in request body",
			},
		})
		return
	}

	// Validate language
	if req.Language == "" {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(internal.ErrorResponse{
			Error: internal.ErrorDetail{
				Code:    internal.ErrUnknownLanguage,
				Message: "language not specified",
			},
		})
		return
	}

	if _, ok := registry.Get(req.Language); !ok {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(internal.ErrorResponse{
			Error: internal.ErrorDetail{
				Code:    internal.ErrUnknownLanguage,
				Message: fmt.Sprintf("unknown language: %s", req.Language),
			},
		})
		return
	}

	// Validate filename
	if req.SourceFile != "" && !internal.ValidSingleFilename(req.SourceFile) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(internal.ErrorResponse{
			Error: internal.ErrorDetail{
				Code:    internal.ErrInvalidFilename,
				Message: "source_filename must be a single path component",
			},
		})
		return
	}
	if req.ArtifactFilename != "" && !internal.ValidSingleFilename(req.ArtifactFilename) {
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(internal.ErrorResponse{
			Error: internal.ErrorDetail{
				Code:    internal.ErrInvalidFilename,
				Message: "artifact_filename must be a single path component",
			},
		})
		return
	}

	// Validate source size
	if len(req.Source) == 0 || len(req.Source) > limitInfo.MaxSourceBytes {
		stats.recordError()
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(internal.ErrorResponse{
			Error: internal.ErrorDetail{
				Code:    internal.ErrInvalidJSON,
				Message: fmt.Sprintf("source must be 1-%d bytes", limitInfo.MaxSourceBytes),
			},
		})
		return
	}

	// Validate test count
	if len(req.Tests) == 0 || len(req.Tests) > limitInfo.MaxTests {
		stats.recordError()
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(internal.ErrorResponse{
			Error: internal.ErrorDetail{
				Code:    internal.ErrTooManyTests,
				Message: fmt.Sprintf("tests must be 1-%d", limitInfo.MaxTests),
			},
		})
		return
	}

	// Validate flags
	lang, _ := registry.Get(req.Language)
	if err := lang.ValidateFlags(req.BuildFlagList()); err != nil {
		stats.recordError()
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(internal.ErrorResponse{
			Error: internal.ErrorDetail{
				Code:    internal.ErrDisallowedFlag,
				Message: err.Error(),
			},
		})
		return
	}
	if err := lang.ValidateFlags(req.RunFlagList()); err != nil {
		stats.recordError()
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(internal.ErrorResponse{
			Error: internal.ErrorDetail{
				Code:    internal.ErrDisallowedFlag,
				Message: err.Error(),
			},
		})
		return
	}

	// Execute
	resp := executor.Execute(&req)

	// Return response
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(resp)
}

// getDiskFree returns free disk space in bytes
func getDiskFree(path string) int64 {
	// Ensure path exists
	if err := os.MkdirAll(path, 0755); err != nil {
		return 0
	}

	// Simple approximation: just return a large number
	// In production, use statfs syscall
	return 1024 * 1024 * 1024 * 100 // 100 GB
}
