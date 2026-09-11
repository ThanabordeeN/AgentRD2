// Package dotenv implements the runtime's small “.env“ loader.
//
// The Python runtime deliberately has no third-party dependencies, so
// “runtime/env.py“ implements only the subset of “python-dotenv“ behaviour
// the project needs. This package is a port of that module:
//
//   - “KEY=value“ lines, with an optional “export “ or Windows “set “
//     prefix
//   - “#“ comments and blank lines
//   - single- or double-quoted values
//   - “${OTHER_KEY}“ interpolation against earlier keys in the same file and
//     then the process environment
//
// Real environment variables always win unless override is set, so a CI runner
// or shell export is never clobbered by a stale local file.
//
// Relative paths are interpreted the way the rest of the runtime interprets
// them: a relative path that exists below the project root (see
// internal/project) names that file, otherwise it is left relative to the
// process working directory.
package dotenv

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/project"
)

var (
	// keyPattern mirrors the Python ``_KEY_RE``: an ASCII identifier.
	keyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	// interpolationPattern mirrors the Python ``_INTERPOLATE_RE``.
	interpolationPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)
)

// DefaultEnvFiles returns the files LoadEnv loads when its paths argument is
// nil, in priority order: ".env", ".env.local" and "config/local.env".
//
// A fresh slice is returned on every call so callers cannot mutate shared
// state.
func DefaultEnvFiles() []string {
	return []string{".env", ".env.local", filepath.Join("config", "local.env")}
}

// EnvFilePath returns the absolute path of an env file inside the project
// root, mirroring “runtime.env.env_file_path“. An empty name means ".env".
func EnvFilePath(name string) string {
	if name == "" {
		name = ".env"
	}
	return project.Resolve(name)
}

// ParseEnvText parses “.env“ content into a plain map.
//
// “${VAR}“ references are resolved from earlier keys in the same text and
// then from the process environment. Unresolvable references resolve to an
// empty string, matching “python-dotenv“.
func ParseEnvText(text string) map[string]string {
	parsed := map[string]string{}
	for _, rawLine := range splitLines(text) {
		line := pyStrip(rawLine)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		lower := strings.ToLower(line)
		switch {
		case strings.HasPrefix(lower, "export "):
			line = pyStrip(line[len("export "):])
		case strings.HasPrefix(lower, "set "):
			line = pyStrip(line[len("set "):])
		}
		if !strings.Contains(line, "=") {
			continue
		}
		key, value, _ := strings.Cut(line, "=")
		key = pyStrip(key)
		if !keyPattern.MatchString(key) {
			continue
		}
		value = pyStrip(value)

		// Strip a trailing comment only for unquoted values.
		if !strings.HasPrefix(value, "'") && !strings.HasPrefix(value, `"`) {
			if index := strings.Index(value, " #"); index != -1 {
				value = pyStrip(value[:index])
			} else if index := strings.Index(value, "\t#"); index != -1 {
				value = pyStrip(value[:index])
			}
		}

		value = stripWrappingQuotes(value)
		value = interpolate(value, parsed)
		parsed[key] = value
	}
	return parsed
}

// LoadEnvFile loads one “.env“ file into the process environment.
//
// It returns the key/value pairs that were actually applied, mirroring
// “runtime.env.load_env_file“: a missing file is not an error and applies
// nothing, and an existing key is only overwritten when override is true.
func LoadEnvFile(path string, override bool) (map[string]string, error) {
	applied := map[string]string{}
	target := resolvePath(path)
	info, err := os.Stat(target)
	if err != nil || info.IsDir() {
		return applied, nil
	}
	data, err := os.ReadFile(target)
	if err != nil {
		return applied, err
	}
	values := ParseEnvText(strings.ToValidUTF8(string(data), "\uFFFD"))
	for key, value := range values {
		if _, exists := os.LookupEnv(key); !override && exists {
			continue
		}
		if err := os.Setenv(key, value); err != nil {
			return applied, err
		}
		applied[key] = value
	}
	return applied, nil
}

// LoadEnv loads the project's env files, skipping any that do not exist.
//
// A nil paths argument selects DefaultEnvFiles, matching the Python
// “load_env“ defaults. The real environment always wins unless override is
// true; without override a key defined by an earlier file is already in the
// environment, so the first file that defines it wins and with override the
// last one does. When a file cannot be read the pairs applied so far are
// returned together with the error.
func LoadEnv(paths []string, override bool) (map[string]string, error) {
	candidates := paths
	if candidates == nil {
		candidates = DefaultEnvFiles()
	}
	applied := map[string]string{}
	for _, candidate := range candidates {
		values, err := LoadEnvFile(candidate, override)
		for key, value := range values {
			applied[key] = value
		}
		if err != nil {
			return applied, err
		}
	}
	return applied, nil
}

// UpsertEnvFile creates or updates “KEY=value“ entries while preserving other
// lines.
//
// Existing keys are rewritten in place, missing keys are appended at the end.
// Comments, blank lines and unrelated keys are left untouched so a hand-edited
// file survives repeated installer runs. New keys are appended in sorted order
// so repeated runs are byte-for-byte deterministic (Python relied on dict
// insertion order, which Go maps do not have).
func UpsertEnvFile(path string, values map[string]string) error {
	target := resolvePath(path)
	if target == "" {
		return fmt.Errorf("dotenv: empty env file path")
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}

	remaining := make(map[string]string, len(values))
	for key, value := range values {
		remaining[key] = value
	}

	output := []string{}
	data, err := os.ReadFile(target)
	switch {
	case err == nil:
		for _, rawLine := range splitLines(strings.ToValidUTF8(string(data), "\uFFFD")) {
			stripped := pyStrip(rawLine)
			body := stripped
			if strings.HasPrefix(strings.ToLower(stripped), "export ") {
				body = pyStrip(stripped[len("export "):])
			}
			key := ""
			if index := strings.Index(body, "="); index != -1 {
				key = pyStrip(body[:index])
			}
			if value, ok := remaining[key]; key != "" && ok {
				output = append(output, key+"="+value)
				delete(remaining, key)
				continue
			}
			output = append(output, rawLine)
		}
	case errors.Is(err, fs.ErrNotExist):
		// A new file: nothing to preserve.
	default:
		return err
	}

	if len(remaining) > 0 {
		if len(output) > 0 && pyStrip(output[len(output)-1]) != "" {
			output = append(output, "")
		}
		keys := make([]string, 0, len(remaining))
		for key := range remaining {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			output = append(output, key+"="+remaining[key])
		}
	}
	return os.WriteFile(target, []byte(strings.Join(output, "\n")+"\n"), 0o644)
}

// interpolate resolves “${VAR}“ references against keys already parsed from
// the same file and then against the process environment.
func interpolate(value string, parsed map[string]string) string {
	if !strings.Contains(value, "${") {
		return value
	}
	return interpolationPattern.ReplaceAllStringFunc(value, func(match string) string {
		name := match[len("${") : len(match)-1]
		if resolved, ok := parsed[name]; ok {
			return resolved
		}
		return os.Getenv(name)
	})
}

// stripWrappingQuotes mirrors “_strip_wrapping_quotes“: a value wrapped in a
// matching pair of single or double quotes loses that pair.
func stripWrappingQuotes(value string) string {
	runes := []rune(value)
	if len(runes) < 2 {
		return value
	}
	first, last := runes[0], runes[len(runes)-1]
	if first != last || (first != '\'' && first != '"') {
		return value
	}
	return string(runes[1 : len(runes)-1])
}

// splitLines splits text exactly like Python's “str.splitlines“: on \n, \r,
// \r\n, \v, \f, \x1c-\x1e, \x85, \u2028 and \u2029, and without producing a
// trailing empty line.
func splitLines(text string) []string {
	lines := []string{}
	start := 0
	for index, current := range text {
		if index < start || !isLineBoundary(current) {
			continue
		}
		lines = append(lines, text[start:index])
		end := index + utf8.RuneLen(current)
		if current == '\r' && end < len(text) && text[end] == '\n' {
			end++
		}
		start = end
	}
	if start < len(text) {
		lines = append(lines, text[start:])
	}
	return lines
}

// isLineBoundary reports whether r terminates a line for Python's splitlines.
func isLineBoundary(r rune) bool {
	switch r {
	case '\n', '\r', '\v', '\f', 0x1c, 0x1d, 0x1e, 0x85, 0x2028, 0x2029:
		return true
	}
	return false
}

// pyStrip trims the characters Python's “str.strip“ removes.
func pyStrip(value string) string {
	return strings.TrimFunc(value, func(r rune) bool {
		return unicode.IsSpace(r) || (r >= 0x1c && r <= 0x1f)
	})
}

// resolvePath prefers a project-root file when the relative path names one, so
// a runtime started from an arbitrary working directory still finds .env. A
// path that only exists relative to the working directory is left for the
// operating system to resolve.
func resolvePath(path string) string {
	if path == "" || filepath.IsAbs(path) {
		return path
	}
	resolved := project.Resolve(path)
	if _, err := os.Stat(resolved); err == nil {
		return resolved
	}
	return path
}
