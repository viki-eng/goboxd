package internal

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type SandboxRunner struct {
	NsjailPath string
	JailDir    string
}

func NewSandboxRunner(nsjailPath string, jailDir string) *SandboxRunner {
	return &SandboxRunner{
		NsjailPath: nsjailPath,
		JailDir:    jailDir,
	}
}

func (sr *SandboxRunner) ExecuteCommand(cmd string, args []string, limits Limits, stdin string, workDir string) (string, string, int64, error) {
	if _, err := os.Stat(sr.NsjailPath); os.IsNotExist(err) {
		return sr.ExecuteCommandDirect(cmd, args, limits, stdin, workDir)
	}
	return sr.ExecuteCommandWithNsjail(cmd, args, limits, stdin, workDir)
}

func (sr *SandboxRunner) ExecuteCommandDirect(cmd string, args []string, limits Limits, stdin string, workDir string) (string, string, int64, error) {
	cmdPath, err := exec.LookPath(cmd)
	if err != nil {
		if cmd == "python3" {
			cmdPath, err = exec.LookPath("python")
			if err != nil {
				return "", "", 0, fmt.Errorf("command not found: %s (also tried python)", cmd)
			}
		} else {
			return "", "", 0, fmt.Errorf("command not found: %s", cmd)
		}
	}
	if cmd == "python3" || cmd == "python" {
		realPythonPath, err := findRealPython()
		if err == nil && realPythonPath != "" {
			cmdPath = realPythonPath
		}
	}
	start := time.Now()
	execCmd := exec.Command(cmdPath, args...)
	execCmd.Dir = workDir
	if stdin != "" {
		execCmd.Stdin = strings.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	execCmd.Stdout = &stdout
	execCmd.Stderr = &stderr
	err = execCmd.Run()
	duration := time.Since(start).Milliseconds()
	const maxOutputBytes = 1024 * 1024
	stdoutStr := truncateOutput(stdout.String(), maxOutputBytes)
	stderrStr := truncateOutput(stderr.String(), maxOutputBytes)
	return stdoutStr, stderrStr, duration, err
}

func findRealPython() (string, error) {
	pathsToTry := []string{
		os.Getenv("PYTHON_EXE"),
		"python.exe",
		"python3.exe",
	}
	username := os.Getenv("USERNAME")
	if username != "" {
		localAppData := fmt.Sprintf("C:\\Users\\%s\\AppData\\Local\\Programs\\Python", username)
		entries, err := os.ReadDir(localAppData)
		if err == nil {
			for _, entry := range entries {
				if entry.IsDir() && strings.Contains(entry.Name(), "Python") {
					pythonExe := filepath.Join(localAppData, entry.Name(), "python.exe")
					if _, err := os.Stat(pythonExe); err == nil {
						return pythonExe, nil
					}
				}
			}
		}
	}
	for _, path := range pathsToTry {
		if path == "" {
			continue
		}
		resolved, err := exec.LookPath(path)
		if err == nil {
			return resolved, nil
		}
	}
	return "", fmt.Errorf("no python executable found")
}

func (sr *SandboxRunner) ExecuteCommandWithNsjail(cmd string, args []string, limits Limits, stdin string, workDir string) (string, string, int64, error) {
	absWorkDir, err := filepath.Abs(workDir)
	if err != nil {
		return "", "", 0, err
	}

	absJailDir, _ := filepath.Abs(sr.JailDir)
	if !strings.HasPrefix(absWorkDir, absJailDir) {
		return "", "", 0, fmt.Errorf("work directory escapes jail")
	}

	// Writable workspace inside jail
	jailWorkDir := "/tmp/work"

	nsjailArgs := []string{
		"-Mo",
		"--quiet",

		"--chroot", "/",
		"--cwd", jailWorkDir,

		"--user", "0",
		"--group", "0",

		// Writable tmpfs
		"--tmpfsmount", "/tmp",

		// Mount host job directory into writable jail path
		"--bindmount", absWorkDir + ":" + jailWorkDir,

		// Environment visible INSIDE jail
		"--env", "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"--env", "TMPDIR=/tmp",
	}

	// Compiler/runtime dependencies
	if strings.Contains(cmd, "g++") ||
		strings.Contains(cmd, "gcc") ||
		strings.Contains(cmd, "clang") ||
		strings.Contains(cmd, "javac") {

		nsjailArgs = append(nsjailArgs,
			"--bindmount_ro", "/usr/bin",
			"--bindmount_ro", "/lib",
			"--bindmount_ro", "/lib64",
			"--bindmount_ro", "/usr/lib",
			"--bindmount_ro", "/usr/lib64",
		)
	}

	// Resource limits
	if limits.WallTimeS > 0 {
		nsjailArgs = append(nsjailArgs,
			"--time_limit", fmt.Sprintf("%d", limits.WallTimeS),
			"--rlimit_cpu", fmt.Sprintf("%d", limits.WallTimeS),
		)
	}

	// FIX #1:
	// nsjail expects KB, not MB.
	if limits.MemoryKb > 0 {
		nsjailArgs = append(
			nsjailArgs,
			"--rlimit_as",
			fmt.Sprintf("%d", limits.MemoryKb),
		)
	}

	if limits.MaxProcesses > 0 {
		nsjailArgs = append(
			nsjailArgs,
			"--rlimit_nproc",
			fmt.Sprintf("%d", limits.MaxProcesses),
		)
	}

	nsjailArgs = append(nsjailArgs, "--", cmd)
	nsjailArgs = append(nsjailArgs, args...)

	log.Printf("DEBUG: nsjail path: %s", sr.NsjailPath)
	log.Printf("DEBUG: workDir: %s", workDir)
	log.Printf("DEBUG: command: %s", cmd)
	log.Printf("DEBUG: args: %v", args)
	log.Printf("DEBUG: full nsjail command: %s %v", sr.NsjailPath, nsjailArgs)

	start := time.Now()

	nsjailCmd := exec.Command(sr.NsjailPath, nsjailArgs...)

	nsjailCmd.Env = os.Environ()

	if stdin != "" {
		nsjailCmd.Stdin = strings.NewReader(stdin)
	}

	var stdout bytes.Buffer
	var stderr bytes.Buffer

	nsjailCmd.Stdout = &stdout
	nsjailCmd.Stderr = &stderr

	err = nsjailCmd.Run()

	duration := time.Since(start).Milliseconds()

	const maxOutputBytes = 1024 * 1024

	stdoutStr := truncateOutput(stdout.String(), maxOutputBytes)
	stderrStr := truncateOutput(stderr.String(), maxOutputBytes)

	return stdoutStr, stderrStr, duration, err
}

func (sr *SandboxRunner) CreateJailDirectory() (string, error) {
	if _, err := os.Stat(sr.JailDir); os.IsNotExist(err) {
		if err := os.MkdirAll(sr.JailDir, 0755); err != nil {
			return "", err
		}
	}
	tempDir, err := os.MkdirTemp(sr.JailDir, "job-*")
	if err != nil {
		return "", err
	}
	return tempDir, nil
}

func (sr *SandboxRunner) CleanupJailDirectory(jailPath string) error {
	absPath, _ := filepath.Abs(jailPath)
	absBase, _ := filepath.Abs(sr.JailDir)
	if !strings.HasPrefix(absPath, absBase) {
		return fmt.Errorf("cleanup path escapes jail")
	}
	return os.RemoveAll(jailPath)
}

func truncateOutput(output string, maxBytes int) string {
	if len(output) <= maxBytes {
		return output
	}
	marker := "\n[output truncated]"
	truncated := output[:maxBytes-len(marker)] + marker
	return truncated
}

func WriteSourceFile(jailPath string, filename string, content string) (string, error) {
	if !ValidSingleFilename(filename) {
		return "", fmt.Errorf("invalid filename")
	}
	fullPath := filepath.Join(jailPath, filename)
	absPath, _ := filepath.Abs(fullPath)
	absJail, _ := filepath.Abs(jailPath)
	if !strings.HasPrefix(absPath, absJail) {
		return "", fmt.Errorf("path traversal attempt blocked")
	}
	if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
		return "", err
	}
	return fullPath, nil
}

func ValidSingleFilename(filename string) bool {
	if filename == "" || filename == "." || strings.HasPrefix(filename, ".") {
		return false
	}
	if strings.Contains(filename, "/") || strings.Contains(filename, "\\") {
		return false
	}
	return filepath.Base(filename) == filename
}

func NsjailVersionFromHelp(help string) string {
	if strings.Contains(help, "Usage:") || strings.Contains(help, "Options:") {
		return "3.4"
	}
	return "unknown"
}

func RunProcess(cmd string, args []string, limits Limits, stdin string, workDir string) (string, string, int64, error) {
	runner := NewSandboxRunner("/usr/bin/nsjail", "/tmp/goboxd-jail")
	return runner.ExecuteCommand(cmd, args, limits, stdin, workDir)
}