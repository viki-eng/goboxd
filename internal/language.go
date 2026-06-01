package internal

import (
	"fmt"
	"os/exec"
	"strings"
)

// Language represents a configured language
type Language struct {
	Id             string
	Name           string
	SourceFilename string
	ArtifactName   string
	BuildCmd       *LanguageCmd
	RunCmd         *LanguageCmd
	FlagAllowlist  map[string]bool
}

// LanguageCmd represents a command to execute (build or run)
type LanguageCmd struct {
	Cmd    string
	Args   []string
	Limits Limits
}

// LanguageRegistry holds all configured languages
type LanguageRegistry struct {
	languages map[string]*Language
	order     []string
}

// NewLanguageRegistry creates an empty registry
func NewLanguageRegistry() *LanguageRegistry {
	return &LanguageRegistry{
		languages: make(map[string]*Language),
		order:     []string{},
	}
}

// Register adds a language to the registry
func (r *LanguageRegistry) Register(lang *Language) error {
	if lang.Id == "" {
		return fmt.Errorf("language ID cannot be empty")
	}
	r.languages[lang.Id] = lang
	r.order = append(r.order, lang.Id)
	return nil
}

// Get returns a language by ID
func (r *LanguageRegistry) Get(id string) (*Language, bool) {
	lang, ok := r.languages[id]
	return lang, ok
}

// All returns all registered languages in order
func (r *LanguageRegistry) All() []*Language {
	result := make([]*Language, len(r.order))
	for i, id := range r.order {
		result[i] = r.languages[id]
	}
	return result
}

// AllIds returns all language IDs in order
func (r *LanguageRegistry) AllIds() []string {
	result := make([]string, len(r.order))
	copy(result, r.order)
	return result
}

// DefaultRegistry returns the built-in language registry for Stage 1
func DefaultRegistry() *LanguageRegistry {
	registry := NewLanguageRegistry()

	// Python 3 – absolute path
	python := &Language{
		Id:             "py3",
		Name:           "Python 3",
		SourceFilename: "solution.py",
		RunCmd: &LanguageCmd{
			Cmd:  "/usr/bin/python3", // ← changed from "python3"
			Args: []string{"{{source}}"},
			Limits: Limits{
				WallTimeS:    9,
				MemoryKb:     102400,
				MaxProcesses: 100,
			},
		},
	}
	registry.Register(python)

	// C++ – absolute paths
	cpp := &Language{
		Id:             "cpp",
		Name:           "C++",
		SourceFilename: "solution.cpp",
		ArtifactName:   "solution",
		BuildCmd: &LanguageCmd{
			Cmd:  "/usr/bin/g++", // ← changed from "g++"
			Args: []string{"{{flags}}", "-o", "{{artifact}}", "{{source}}"},
			Limits: Limits{
				WallTimeS:    3,
				MemoryKb:     1048576,
				MaxProcesses: 100,
			},
		},
		RunCmd: &LanguageCmd{
			Cmd:  "./{{artifact}}", // ← relative is fine (inside jail)
			Args: []string{},
			Limits: Limits{
				WallTimeS:    3,
				MemoryKb:     524288,
				MaxProcesses: 64,
			},
		},
		FlagAllowlist: map[string]bool{
			"-O0": true, "-O1": true, "-O2": true, "-O3": true,
			"-Wall": true, "-Wextra": true,
		},
	}
	registry.Register(cpp)

	return registry
}

// CheckLanguageAvailability verifies a language's tools are installed
func CheckLanguageAvailability(lang *Language) (bool, string, error) {
	var checkPath string

	// For compiled languages, check build tool; for interpreted, check run tool
	if lang.BuildCmd != nil {
		// Compiled language - check build command
		checkPath = lang.BuildCmd.Cmd
	} else if lang.RunCmd != nil {
		// Interpreted language - check run command
		checkPath = lang.RunCmd.Cmd
	}

	// Skip templates (like ./{{artifact}})
	if strings.Contains(checkPath, "{{") {
		return true, "", nil
	}

	// Check if executable exists
	if checkPath != "" {
		if _, err := exec.LookPath(checkPath); err != nil {
			return false, "", fmt.Errorf("tool not found: %s", checkPath)
		}

		// Get version (try --version)
		cmd := exec.Command(checkPath, "--version")
		out, err := cmd.Output()
		if err != nil {
			// If --version fails, just mark as available
			return true, "", nil
		}
		version := strings.TrimSpace(string(out))
		return true, version, nil
	}

	return true, "", nil
}

// ValidateFlags checks if flags are allowed by the language
func (lang *Language) ValidateFlags(flags []string) error {
	if lang.FlagAllowlist == nil || len(lang.FlagAllowlist) == 0 {
		// No allowlist means no custom flags allowed
		if len(flags) > 0 {
			return fmt.Errorf("custom flags not allowed for %s", lang.Id)
		}
		return nil
	}

	for _, flag := range flags {
		allowed := false
		// Check exact match or wildcard patterns
		for allowedFlag := range lang.FlagAllowlist {
			if allowedFlag == flag {
				allowed = true
				break
			}
			// Simple wildcard matching for patterns like "-std=*"
			if strings.HasSuffix(allowedFlag, "*") {
				prefix := strings.TrimSuffix(allowedFlag, "*")
				if strings.HasPrefix(flag, prefix) {
					allowed = true
					break
				}
			}
		}
		if !allowed {
			return fmt.Errorf("flag '%s' not allowed for %s", flag, lang.Id)
		}
	}
	return nil
}
