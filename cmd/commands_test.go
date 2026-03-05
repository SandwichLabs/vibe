package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockReadPassword returns a function that simulates terminal passphrase input.
func mockReadPassword(pass string) func(fd int) ([]byte, error) {
	return func(fd int) ([]byte, error) {
		return []byte(pass), nil
	}
}

// executeCommand runs the root command with the given args and returns stdout output and error.
// It resets flag values between invocations to prevent leakage.
func executeCommand(args ...string) (string, error) {
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(new(bytes.Buffer))
	rootCmd.SetArgs(args)

	// Reset flag values to defaults (without clearing flag definitions).
	initCmd.Flags().Set("title", "")       //nolint:errcheck
	initCmd.Flags().Set("description", "") //nolint:errcheck
	initCmd.Flags().Set("from", "")        //nolint:errcheck
	initCmd.Flags().Set("import", "")      //nolint:errcheck
	infoCmd.Flags().Set("json", "false")   //nolint:errcheck

	err := rootCmd.Execute()
	return buf.String(), err
}

func TestExportCommand_RequiresTable(t *testing.T) {
	output, err := executeCommand("export", "test.vibe")
	_ = output
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--table flag is required")
}

func TestImportCommand_RequiresFlags(t *testing.T) {
	_, err := executeCommand("import", "test.vibe")
	assert.Error(t, err)
}

func TestInitCommand_CreatesFile(t *testing.T) {
	origReader := readPasswordFunc
	readPasswordFunc = mockReadPassword("testpass123")
	defer func() { readPasswordFunc = origReader }()

	dir := t.TempDir()
	filePath := filepath.Join(dir, "test.vibe")

	output, err := executeCommand("init", filePath)
	require.NoError(t, err)

	_, statErr := os.Stat(filePath)
	require.NoError(t, statErr)

	assert.Contains(t, output, "Created")
	assert.Contains(t, output, "test")
}

func TestInitCommand_WithTitle(t *testing.T) {
	origReader := readPasswordFunc
	readPasswordFunc = mockReadPassword("testpass123")
	defer func() { readPasswordFunc = origReader }()

	dir := t.TempDir()
	filePath := filepath.Join(dir, "titled.vibe")

	output, err := executeCommand("init", filePath, "--title", "My App")
	require.NoError(t, err)

	assert.Contains(t, output, `"My App"`)
}

func TestInitCommand_WithDescription(t *testing.T) {
	origReader := readPasswordFunc
	readPasswordFunc = mockReadPassword("testpass123")
	defer func() { readPasswordFunc = origReader }()

	dir := t.TempDir()
	filePath := filepath.Join(dir, "described.vibe")

	output, err := executeCommand("init", filePath, "--description", "A test container")
	require.NoError(t, err)

	assert.Contains(t, output, "Created")
}

func TestInitCommand_AddsVibeExtension(t *testing.T) {
	origReader := readPasswordFunc
	readPasswordFunc = mockReadPassword("testpass123")
	defer func() { readPasswordFunc = origReader }()

	dir := t.TempDir()
	filePath := filepath.Join(dir, "noext")

	_, err := executeCommand("init", filePath)
	require.NoError(t, err)

	_, statErr := os.Stat(filePath + ".vibe")
	require.NoError(t, statErr)
}

func TestInitCommand_FromTemplate(t *testing.T) {
	origReader := readPasswordFunc
	readPasswordFunc = mockReadPassword("testpass123")
	defer func() { readPasswordFunc = origReader }()

	dir := t.TempDir()

	// Create a template first.
	templatePath := filepath.Join(dir, "template.vibe")
	_, err := executeCommand("init", templatePath, "--title", "Template")
	require.NoError(t, err)

	// Create from template.
	newPath := filepath.Join(dir, "clone.vibe")
	output, err := executeCommand("init", newPath, "--from", templatePath, "--title", "Clone")
	require.NoError(t, err)

	assert.Contains(t, output, "from template")
	assert.Contains(t, output, "Created")

	_, statErr := os.Stat(newPath)
	require.NoError(t, statErr)
}

func TestInitCommand_ImportCSV(t *testing.T) {
	origReader := readPasswordFunc
	readPasswordFunc = mockReadPassword("testpass123")
	defer func() { readPasswordFunc = origReader }()

	dir := t.TempDir()

	csvPath := filepath.Join(dir, "data.csv")
	err := os.WriteFile(csvPath, []byte("name,age\nAlice,30\nBob,25\n"), 0644)
	require.NoError(t, err)

	filePath := filepath.Join(dir, "imported.vibe")
	output, err := executeCommand("init", filePath, "--import", csvPath)
	require.NoError(t, err)

	assert.Contains(t, output, "Imported 2 rows")
	assert.Contains(t, output, "Created")
}

func TestInitCommand_FromAndImportMutuallyExclusive(t *testing.T) {
	origReader := readPasswordFunc
	readPasswordFunc = mockReadPassword("testpass123")
	defer func() { readPasswordFunc = origReader }()

	dir := t.TempDir()
	filePath := filepath.Join(dir, "test.vibe")

	_, err := executeCommand("init", filePath, "--from", "template.vibe", "--import", "data.csv")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "mutually exclusive")
}

func TestInfoCommand_ShowsMetadata(t *testing.T) {
	origReader := readPasswordFunc
	readPasswordFunc = mockReadPassword("testpass123")
	defer func() { readPasswordFunc = origReader }()

	dir := t.TempDir()

	// Create a file first.
	filePath := filepath.Join(dir, "info-test.vibe")
	_, err := executeCommand("init", filePath, "--title", "Info Test")
	require.NoError(t, err)

	// Run info.
	output, err := executeCommand("info", filePath)
	require.NoError(t, err)

	assert.Contains(t, output, "File:")
	assert.Contains(t, output, "Size:")
	assert.Contains(t, output, "Info Test")
	assert.Contains(t, output, "Pages:")
	assert.Contains(t, output, "Tables:")
}

func TestInfoCommand_JSON(t *testing.T) {
	origReader := readPasswordFunc
	readPasswordFunc = mockReadPassword("testpass123")
	defer func() { readPasswordFunc = origReader }()

	dir := t.TempDir()

	// Create a file first.
	filePath := filepath.Join(dir, "json-test.vibe")
	_, err := executeCommand("init", filePath, "--title", "JSON Test")
	require.NoError(t, err)

	// Run info --json.
	output, err := executeCommand("info", filePath, "--json")
	require.NoError(t, err)

	var result map[string]any
	err = json.Unmarshal([]byte(output), &result)
	require.NoError(t, err)

	assert.Equal(t, "JSON Test", result["title"])
	assert.NotNil(t, result["size"])
	assert.NotNil(t, result["pages"])
	assert.NotNil(t, result["tables"])
}
