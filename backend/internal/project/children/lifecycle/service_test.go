package lifecycle

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeLifecycleProjectDirWithScript(t *testing.T, scriptRel, scriptBody string) string {
	t.Helper()
	dir := t.TempDir()
	if scriptRel != "" {
		full := filepath.Join(dir, scriptRel)
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
		require.NoError(t, os.WriteFile(full, []byte(scriptBody), 0o644))
	}
	return dir
}

func TestParseKeyValueEnv_HappyPath(t *testing.T) {
	stdout := "FOO=bar\n# comment line\n\nBAZ_2=hello world\n"
	got, err := ParseKeyValueEnv(stdout)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"FOO": "bar", "BAZ_2": "hello world"}, got)
}

func TestParseKeyValueEnv_RejectsMalformedLines(t *testing.T) {
	cases := map[string]string{
		"no equals":   "FOO bar",
		"empty key":   "=value",
		"bad key":     "foo-bar=baz",
		"digit start": "1FOO=bar",
	}
	for name, stdout := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := ParseKeyValueEnv(stdout)
			require.Error(t, err)
		})
	}
}

func TestParseKeyValueEnv_AllowsEqualsInValue(t *testing.T) {
	stdout := "TOKEN=abc=def=ghi"
	got, err := ParseKeyValueEnv(stdout)
	require.NoError(t, err)
	assert.Equal(t, "abc=def=ghi", got["TOKEN"])
}

func TestValidateScriptPath_HappyPath(t *testing.T) {
	dir := writeLifecycleProjectDirWithScript(t, "scripts/pre-deploy.sh", "#!/bin/sh\necho hi\n")
	err := ValidateScriptPath(t.Context(), dir, "scripts/pre-deploy.sh")
	require.NoError(t, err)
}

func TestValidateScriptPath_RejectsAbsolute(t *testing.T) {
	dir := t.TempDir()
	err := ValidateScriptPath(t.Context(), dir, "/etc/passwd")
	require.Error(t, err)
}

func TestValidateScriptPath_RejectsTraversal(t *testing.T) {
	dir := t.TempDir()
	err := ValidateScriptPath(t.Context(), dir, "../escape.sh")
	require.Error(t, err)
}

func TestValidateScriptPath_RejectsMissingFile(t *testing.T) {
	dir := t.TempDir()
	err := ValidateScriptPath(t.Context(), dir, "missing.sh")
	require.Error(t, err)
}

func TestValidateScriptPath_PermissionDeniedProceeds(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root; directory permissions are not enforced")
	}
	dir := writeLifecycleProjectDirWithScript(t, "scripts/pre-deploy.sh", "#!/bin/sh\necho hi\n")
	scripts := filepath.Join(dir, "scripts")
	require.NoError(t, os.Chmod(scripts, 0o000))
	t.Cleanup(func() { require.NoError(t, os.Chmod(scripts, 0o755)) })

	// The runner container may still be able to read the script even though
	// Arcane's uid cannot (#3373), so the stat failure must not block the run.
	err := ValidateScriptPath(t.Context(), dir, "scripts/pre-deploy.sh")
	require.NoError(t, err)
}

func TestValidateScriptPath_RejectsDirectory(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "scripts"), 0o755))
	err := ValidateScriptPath(t.Context(), dir, "scripts")
	require.Error(t, err)
}

func TestValidateScriptPath_RejectsSymlink(t *testing.T) {
	if _, err := os.Stat("/tmp"); err != nil {
		t.Skip("symlink test requires unix-like FS")
	}
	dir := t.TempDir()
	target := filepath.Join(dir, "real.sh")
	require.NoError(t, os.WriteFile(target, []byte("echo"), 0o644))
	link := filepath.Join(dir, "link.sh")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("cannot create symlink in test env: %v", err)
	}
	err := ValidateScriptPath(t.Context(), dir, "link.sh")
	require.Error(t, err)
}

func TestCombineLifecycleOutput(t *testing.T) {
	cases := []struct {
		stdout, stderr, want string
	}{
		{"", "", ""},
		{"hello", "", "hello"},
		{"", "boom", "--- stderr ---\nboom"},
		{"hello\n", "boom\n", "hello\n--- stderr ---\nboom"},
	}
	for _, c := range cases {
		got := CombineLifecycleOutput(c.stdout, c.stderr)
		assert.Equal(t, c.want, got, "stdout=%q stderr=%q", c.stdout, c.stderr)
	}
}
