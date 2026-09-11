package dotenv

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDefaultEnvFiles(t *testing.T) {
	want := []string{".env", ".env.local", filepath.Join("config", "local.env")}
	got := DefaultEnvFiles()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DefaultEnvFiles() = %v, want %v", got, want)
	}
	got[0] = "mutated"
	if DefaultEnvFiles()[0] != ".env" {
		t.Fatal("DefaultEnvFiles must return a fresh slice on every call")
	}
}

func TestEnvFilePathIsAbsoluteAndProjectRelative(t *testing.T) {
	path := EnvFilePath("")
	if !filepath.IsAbs(path) || filepath.Base(path) != ".env" {
		t.Fatalf("EnvFilePath(\"\") = %q, want an absolute .../.env", path)
	}
	if custom := EnvFilePath("config/local.env"); filepath.Base(custom) != "local.env" {
		t.Fatalf("EnvFilePath(\"config/local.env\") = %q", custom)
	}
}

func TestParseEnvTextBasicAssignmentsAndComments(t *testing.T) {
	text := "# a comment\n" +
		"\n" +
		"OPENCODE_API_KEY=sk-abc123\n" +
		"  RDR2AI_MODEL = deepseek-v4.1-flash  \n" +
		"export RDR2AI_BACKEND=llm\n"
	parsed := ParseEnvText(text)
	for key, want := range map[string]string{
		"OPENCODE_API_KEY": "sk-abc123",
		"RDR2AI_MODEL":     "deepseek-v4.1-flash",
		"RDR2AI_BACKEND":   "llm",
	} {
		if got := parsed[key]; got != want {
			t.Errorf("parsed[%q] = %q, want %q", key, got, want)
		}
	}
	if len(parsed) != 3 {
		t.Fatalf("parsed %d keys, want 3: %v", len(parsed), parsed)
	}
}

func TestParseEnvTextHandlesQuotesAndInlineComments(t *testing.T) {
	text := "QUOTED=\"value with spaces\"\n" +
		"SINGLE='another value'\n" +
		"PLAIN=bare # trailing comment\n" +
		"TABBED=also bare\t# trailing comment\n" +
		"URL=https://example.com/v1#fragment\n" +
		"NOT_A_PAIR\n" +
		"=missing-key\n"
	parsed := ParseEnvText(text)
	for key, want := range map[string]string{
		"QUOTED": "value with spaces",
		"SINGLE": "another value",
		"PLAIN":  "bare",
		"TABBED": "also bare",
		"URL":    "https://example.com/v1#fragment",
	} {
		if got := parsed[key]; got != want {
			t.Errorf("parsed[%q] = %q, want %q", key, got, want)
		}
	}
	if _, ok := parsed["NOT_A_PAIR"]; ok {
		t.Error("a line without '=' must be ignored")
	}
	if _, ok := parsed[""]; ok {
		t.Error("an empty key must be ignored")
	}
	if _, ok := parsed["missing-key"]; ok {
		t.Error("'=missing-key' must not register a bare key")
	}
}

func TestParseEnvTextWindowsSetPrefixAndCaseInsensitivePrefixes(t *testing.T) {
	parsed := ParseEnvText("set WINDOWS_STYLE=1\nEXPORT UPPER_STYLE=2\n  Export  Spaced = 3  \n")
	for key, want := range map[string]string{
		"WINDOWS_STYLE": "1",
		"UPPER_STYLE":   "2",
		"Spaced":        "3",
	} {
		if got := parsed[key]; got != want {
			t.Errorf("parsed[%q] = %q, want %q", key, got, want)
		}
	}
}

func TestParseEnvTextLineEndings(t *testing.T) {
	parsed := ParseEnvText("A=1\r\nB=2\rC=3\n\n")
	for key, want := range map[string]string{"A": "1", "B": "2", "C": "3"} {
		if got := parsed[key]; got != want {
			t.Errorf("parsed[%q] = %q, want %q", key, got, want)
		}
	}
	if len(parsed) != 3 {
		t.Fatalf("parsed %d keys, want 3: %v", len(parsed), parsed)
	}
}

func TestParseEnvTextInterpolationUsesEarlierKeysThenEnvironment(t *testing.T) {
	t.Setenv("EXTERNAL_HOST", "api.example.com")
	unsetEnv(t, "NOPE_UNDEFINED")

	parsed := ParseEnvText(
		"BASE=${EXTERNAL_HOST}/v1\n" +
			"FULL=${BASE}/chat\n" +
			"QUOTED=\"${BASE}\"\n" +
			"MISSING=${NOPE_UNDEFINED}\n",
	)
	for key, want := range map[string]string{
		"BASE":    "api.example.com/v1",
		"FULL":    "api.example.com/v1/chat",
		"QUOTED":  "api.example.com/v1",
		"MISSING": "",
	} {
		if got := parsed[key]; got != want {
			t.Errorf("parsed[%q] = %q, want %q", key, got, want)
		}
	}
}

func TestParseEnvTextLastAssignmentWins(t *testing.T) {
	parsed := ParseEnvText("A=first\nA=second\nB=${A}\n")
	if parsed["A"] != "second" || parsed["B"] != "second" {
		t.Fatalf("parsed = %v, want A and B to be \"second\"", parsed)
	}
}

func TestLoadEnvFileDoesNotClobberRealEnvironment(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	if err := os.WriteFile(path, []byte("KEEP_ME=from-file\nADD_ME=from-file\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KEEP_ME", "from-shell")
	unsetEnv(t, "ADD_ME")

	applied, err := LoadEnvFile(path, false)
	if err != nil {
		t.Fatalf("LoadEnvFile: %v", err)
	}
	if got := os.Getenv("KEEP_ME"); got != "from-shell" {
		t.Errorf("KEEP_ME = %q, want the shell value", got)
	}
	if got := os.Getenv("ADD_ME"); got != "from-file" {
		t.Errorf("ADD_ME = %q, want the file value", got)
	}
	if want := map[string]string{"ADD_ME": "from-file"}; !reflect.DeepEqual(applied, want) {
		t.Errorf("applied = %v, want %v", applied, want)
	}

	overrideApplied, err := LoadEnvFile(path, true)
	if err != nil {
		t.Fatalf("LoadEnvFile(override): %v", err)
	}
	if got := os.Getenv("KEEP_ME"); got != "from-file" {
		t.Errorf("KEEP_ME = %q, want the file value after override", got)
	}
	want := map[string]string{"KEEP_ME": "from-file", "ADD_ME": "from-file"}
	if !reflect.DeepEqual(overrideApplied, want) {
		t.Errorf("override applied = %v, want %v", overrideApplied, want)
	}
}

func TestLoadEnvFileMissingOrDirectoryIsNotAnError(t *testing.T) {
	dir := t.TempDir()
	for _, path := range []string{filepath.Join(dir, "absent.env"), dir} {
		applied, err := LoadEnvFile(path, false)
		if err != nil {
			t.Fatalf("LoadEnvFile(%q): %v", path, err)
		}
		if len(applied) != 0 {
			t.Errorf("LoadEnvFile(%q) applied %v, want nothing", path, applied)
		}
		if applied == nil {
			t.Errorf("LoadEnvFile(%q) returned a nil map, want an empty one", path)
		}
	}
}

func TestLoadEnvSearchesCandidatesInOrder(t *testing.T) {
	dir := t.TempDir()
	base := filepath.Join(dir, ".env")
	local := filepath.Join(dir, "local.env")
	if err := os.WriteFile(base, []byte("FROM_BASE=1\nSHARED=base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(local, []byte("FROM_LOCAL=2\nSHARED=local\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"FROM_BASE", "FROM_LOCAL", "SHARED"} {
		unsetEnv(t, name)
	}

	// Without override a key is only applied once, so the *first* file that
	// defines it wins: every later candidate sees it in the environment.
	applied, err := LoadEnv([]string{base, local}, false)
	if err != nil {
		t.Fatalf("LoadEnv: %v", err)
	}
	want := map[string]string{"FROM_BASE": "1", "FROM_LOCAL": "2", "SHARED": "base"}
	if !reflect.DeepEqual(applied, want) {
		t.Errorf("applied = %v, want %v", applied, want)
	}
	if got := os.Getenv("SHARED"); got != "base" {
		t.Errorf("SHARED = %q, want the first file to win without override", got)
	}

	// With override the later file wins.
	overrideApplied, err := LoadEnv([]string{base, local}, true)
	if err != nil {
		t.Fatalf("LoadEnv(override): %v", err)
	}
	want = map[string]string{"FROM_BASE": "1", "FROM_LOCAL": "2", "SHARED": "local"}
	if !reflect.DeepEqual(overrideApplied, want) {
		t.Errorf("override applied = %v, want %v", overrideApplied, want)
	}
	if got := os.Getenv("SHARED"); got != "local" {
		t.Errorf("SHARED = %q, want the later file to win with override", got)
	}

	missing, err := LoadEnv([]string{filepath.Join(dir, "absent.env")}, false)
	if err != nil {
		t.Fatalf("LoadEnv with a missing candidate: %v", err)
	}
	if len(missing) != 0 {
		t.Errorf("missing candidates applied %v, want nothing", missing)
	}
}

func TestLoadEnvNilPathsUsesProjectDefaults(t *testing.T) {
	// The repository has no .env files, but even when a developer has some the
	// call must succeed: LoadEnv only reports real read failures.
	applied, err := LoadEnv(nil, false)
	if err != nil {
		t.Fatalf("LoadEnv(nil): %v", err)
	}
	for key, value := range applied {
		if got := os.Getenv(key); got != value {
			t.Errorf("%s = %q, want the applied value %q", key, got, value)
		}
	}
	// An explicitly empty (non-nil) list means "load nothing".
	empty, err := LoadEnv([]string{}, false)
	if err != nil {
		t.Fatalf("LoadEnv(empty): %v", err)
	}
	if len(empty) != 0 {
		t.Errorf("LoadEnv([]string{}) applied %v, want nothing", empty)
	}
}

func TestUpsertEnvFileCreatesFileWithValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", ".env")
	if err := UpsertEnvFile(path, map[string]string{"OPENCODE_API_KEY": "sk-1"}); err != nil {
		t.Fatalf("UpsertEnvFile: %v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("the file must exist: %v", err)
	}
	if !strings.Contains(string(content), "OPENCODE_API_KEY=sk-1") {
		t.Fatalf("content = %q, want it to contain the key", content)
	}
	if !strings.HasSuffix(string(content), "\n") {
		t.Errorf("content = %q, want a trailing newline", content)
	}
}

func TestUpsertEnvFileUpdatesInPlaceAndPreservesComments(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	original := "# keep this comment\nOPENCODE_API_KEY=old\nOTHER=untouched\nexport OLD_STYLE=1\n\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	err := UpsertEnvFile(path, map[string]string{
		"OPENCODE_API_KEY": "new",
		"RDR2AI_MODEL":     "m",
		"OLD_STYLE":        "2",
	})
	if err != nil {
		t.Fatalf("UpsertEnvFile: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{
		"# keep this comment",
		"OPENCODE_API_KEY=new",
		"OTHER=untouched",
		"OLD_STYLE=2",
		"RDR2AI_MODEL=m",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("content missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "OPENCODE_API_KEY=old") {
		t.Errorf("the old value survived:\n%s", text)
	}
	if strings.Contains(text, "export OLD_STYLE=2") {
		t.Errorf("the export prefix must be dropped when rewriting:\n%s", text)
	}
	// The rewritten keys stay where they were; only the new key is appended.
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if lines[1] != "OPENCODE_API_KEY=new" || lines[3] != "OLD_STYLE=2" {
		t.Errorf("existing keys must be rewritten in place, got:\n%s", text)
	}
	if lines[len(lines)-1] != "RDR2AI_MODEL=m" {
		t.Errorf("new keys must be appended, got:\n%s", text)
	}
}

func TestUpsertEnvFileIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	values := map[string]string{"A": "1", "B": "2"}
	if err := UpsertEnvFile(path, values); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := UpsertEnvFile(path, values); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("upsert is not idempotent:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

func TestUpsertEnvFileAppendsNewKeysDeterministically(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	values := map[string]string{"ZED": "1", "ALPHA": "2", "MIKE": "3"}
	for run := 0; run < 5; run++ {
		if err := UpsertEnvFile(path, values); err != nil {
			t.Fatal(err)
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "ALPHA=2\nMIKE=3\nZED=1\n"
	if string(raw) != want {
		t.Fatalf("content = %q, want %q", raw, want)
	}
}

func TestUpsertEnvFileLeavesSetPrefixedLinesAlone(t *testing.T) {
	// Python's upsert only understands the "export " prefix; a Windows-style
	// "set " line has the key "set KEY" and must be preserved verbatim.
	path := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(path, []byte("set WINDOWS=old\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := UpsertEnvFile(path, map[string]string{"WINDOWS": "new"}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if !strings.Contains(text, "set WINDOWS=old") {
		t.Errorf("the set-prefixed line must be preserved:\n%s", text)
	}
	if !strings.Contains(text, "WINDOWS=new") {
		t.Errorf("the new value must still be appended:\n%s", text)
	}
}

func TestUpsertEnvFileWritesEmptyFileWhenThereIsNothingToWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	if err := UpsertEnvFile(path, map[string]string{}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "\n" {
		t.Fatalf("content = %q, want a single newline", raw)
	}
}

// unsetEnv removes name for the duration of the test, restoring the previous
// value afterwards.
func unsetEnv(t *testing.T, name string) {
	t.Helper()
	previous, existed := os.LookupEnv(name)
	if err := os.Unsetenv(name); err != nil {
		t.Fatalf("os.Unsetenv(%q): %v", name, err)
	}
	t.Cleanup(func() {
		if existed {
			_ = os.Setenv(name, previous)
			return
		}
		_ = os.Unsetenv(name)
	})
}
