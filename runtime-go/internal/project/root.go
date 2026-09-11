// Package project resolves where the runtime's data lives.
//
// The Python runtime derived this from ``__file__``. A Go binary may also be
// shipped as a standalone executable, in which case configuration, profiles,
// and the wiki packs sit next to the executable rather than next to the
// source tree. Both cases resolve to the same answer here.
package project

import (
	"os"
	"path/filepath"
	"sync"
)

var (
	once       sync.Once
	cachedRoot string
)

// Root returns the absolute directory that holds config/ and data/.
//
// Resolution order:
//  1. RDR2AI_ROOT, when set (useful for tests and portable installs)
//  2. the executable's directory, when it looks like a packaged install
//     (it contains config/settings.json or data/)
//  3. the repository root inferred from the current working directory or the
//     compiled-in source path
func Root() string {
	once.Do(func() {
		cachedRoot = resolveRoot()
	})
	return cachedRoot
}

// SetRoot overrides the cached root. Intended for tests.
func SetRoot(path string) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		absolute = path
	}
	cachedRoot = absolute
	once.Do(func() {})
}

func resolveRoot() string {
	if override := os.Getenv("RDR2AI_ROOT"); override != "" {
		if absolute, err := filepath.Abs(override); err == nil {
			return absolute
		}
		return override
	}

	if executable, err := os.Executable(); err == nil {
		dir := filepath.Dir(executable)
		if looksLikeRoot(dir) {
			return dir
		}
	}

	if cwd, err := os.Getwd(); err == nil {
		for dir := cwd; ; {
			if looksLikeRoot(dir) {
				return dir
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}

	if cwd, err := os.Getwd(); err == nil {
		return cwd
	}
	return "."
}

// looksLikeRoot reports whether dir contains the runtime's data layout.
func looksLikeRoot(dir string) bool {
	for _, marker := range []string{
		filepath.Join("config", "settings.json"),
		filepath.Join("data", "profiles"),
	} {
		if _, err := os.Stat(filepath.Join(dir, marker)); err == nil {
			return true
		}
	}
	return false
}

// Resolve returns an absolute path for a project-relative path. Absolute
// inputs are returned unchanged.
func Resolve(path string) string {
	if path == "" {
		return ""
	}
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(Root(), path)
}
