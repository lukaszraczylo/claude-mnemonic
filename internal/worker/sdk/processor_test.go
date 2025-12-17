package sdk

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
	"github.com/stretchr/testify/assert"
)

func TestIsSelfReferentialSummary(t *testing.T) {
	tests := []struct {
		name     string
		summary  *models.ParsedSummary
		expected bool
	}{
		{
			name: "meta summary about memory agent role",
			summary: &models.ParsedSummary{
				Request:   "Memory extraction agent role - analyze tool executions and extract meaningful observations for future sessions",
				Completed: "No work has been completed yet. The session has just started with the user providing role definition and operational guidelines.",
				Learned:   "The system expects observations to be created from meaningful learnings during Claude Code sessions, with focus on decisions, bugs fixed, patterns discovered, project structure changes, and code modifications.",
				NextSteps: "Awaiting tool executions or user requests that contain actual work performed in a Claude Code session.",
			},
			expected: true,
		},
		{
			name: "legitimate summary about code changes",
			summary: &models.ParsedSummary{
				Request:   "Fix authentication bug in login handler",
				Completed: "Updated the auth middleware to properly validate JWT tokens and fixed the session expiry check.",
				Learned:   "The JWT library requires explicit algorithm validation to prevent token substitution attacks.",
				NextSteps: "Add unit tests for the authentication flow.",
			},
			expected: false,
		},
		{
			name: "awaiting user summary",
			summary: &models.ParsedSummary{
				Request:   "Session initialization",
				Completed: "No work completed yet.",
				Learned:   "Awaiting user input to begin work.",
				NextSteps: "Waiting for the user to provide instructions.",
			},
			expected: true,
		},
		{
			name: "summary about refactoring",
			summary: &models.ParsedSummary{
				Request:   "Refactor database connection pooling",
				Completed: "Implemented connection pooling using pgxpool with max 10 connections.",
				Learned:   "pgxpool automatically handles connection reuse and health checks.",
				NextSteps: "Run benchmarks to verify performance improvement.",
			},
			expected: false,
		},
		{
			name: "meta summary with extraction agent mention",
			summary: &models.ParsedSummary{
				Request:   "Extraction agent initialization",
				Completed: "No substantive work has been done.",
				Learned:   "The memory extraction agent analyzes tool executions.",
				NextSteps: "Awaiting tool results to extract observations.",
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isSelfReferentialSummary(tt.summary)
			if result != tt.expected {
				t.Errorf("isSelfReferentialSummary() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestHasMeaningfulContent(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected bool
	}{
		{
			name:     "empty content",
			content:  "",
			expected: false,
		},
		{
			name:     "too short content",
			content:  "Hello world",
			expected: false,
		},
		{
			name: "meta content about memory agent",
			content: `This is the memory extraction agent role definition.
The system expects you to analyze tool executions and extract meaningful observations.
No work has been completed yet. Awaiting tool results from the user's session.`,
			expected: false,
		},
		{
			name: "legitimate code discussion",
			content: `I've updated the handler.go file to fix the authentication bug.
The function validateToken() was not checking token expiry correctly.
I've added a check for exp claim and implemented proper error handling.
The changes have been tested and the build passes.`,
			expected: true,
		},
		{
			name: "hook status messages",
			content: `SessionStart:Callback hook success: Success
The memory agent is waiting for user input.
System-reminder about available tools.
No substantive work performed yet.`,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasMeaningfulContent(tt.content)
			if result != tt.expected {
				t.Errorf("hasMeaningfulContent() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestShouldSkipTool(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		expected bool
	}{
		// Tools that should be skipped
		{"TodoWrite", "TodoWrite", true},
		{"Task", "Task", true},
		{"TaskOutput", "TaskOutput", true},
		{"Glob", "Glob", true},
		{"ListDir", "ListDir", true},
		{"LS", "LS", true},
		{"KillShell", "KillShell", true},
		{"AskUserQuestion", "AskUserQuestion", true},
		{"EnterPlanMode", "EnterPlanMode", true},
		{"ExitPlanMode", "ExitPlanMode", true},
		{"Skill", "Skill", true},
		{"SlashCommand", "SlashCommand", true},

		// Tools that should NOT be skipped
		{"Read", "Read", false},
		{"Edit", "Edit", false},
		{"Write", "Write", false},
		{"Grep", "Grep", false},
		{"Bash", "Bash", false},
		{"WebFetch", "WebFetch", false},
		{"WebSearch", "WebSearch", false},
		{"NotebookEdit", "NotebookEdit", false},

		// Unknown tool (should not be skipped)
		{"UnknownTool", "SomeUnknownTool", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := shouldSkipTool(tt.toolName)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestShouldSkipTrivialOperation(t *testing.T) {
	tests := []struct {
		name      string
		toolName  string
		inputStr  string
		outputStr string
		expected  bool
	}{
		// Short output (should be skipped)
		{
			name:      "output_too_short",
			toolName:  "Read",
			inputStr:  `{"file_path": "/some/file.go"}`,
			outputStr: "short",
			expected:  true,
		},
		// Trivial outputs
		{
			name:      "no_matches_found",
			toolName:  "Grep",
			inputStr:  `{"pattern": "foo"}`,
			outputStr: "No matches found in the codebase",
			expected:  true,
		},
		{
			name:      "file_not_found",
			toolName:  "Read",
			inputStr:  `{"file_path": "/nonexistent.go"}`,
			outputStr: "Error: File not found at specified path",
			expected:  true,
		},
		{
			name:      "empty_array",
			toolName:  "Grep",
			inputStr:  `{"pattern": "foo"}`,
			outputStr: "[]",
			expected:  true,
		},
		// Boring files
		{
			name:      "package_lock_json",
			toolName:  "Read",
			inputStr:  `{"file_path": "/project/package-lock.json"}`,
			outputStr: "This is a very long package-lock.json content that has more than 50 characters",
			expected:  true,
		},
		{
			name:      "go_sum",
			toolName:  "Read",
			inputStr:  `{"file_path": "/project/go.sum"}`,
			outputStr: "This is a very long go.sum file content that has more than 50 characters",
			expected:  true,
		},
		// Grep with too many matches
		{
			name:     "grep_too_many_matches",
			toolName: "Grep",
			inputStr: `{"pattern": "import"}`,
			outputStr: func() string {
				s := ""
				for i := 0; i < 55; i++ {
					s += "match line\n"
				}
				return s
			}(),
			expected: true,
		},
		// Boring Bash commands
		{
			name:      "git_status",
			toolName:  "Bash",
			inputStr:  `{"command": "git status"}`,
			outputStr: "On branch main\nYour branch is up to date with 'origin/main'.\nnothing to commit, working tree clean",
			expected:  true,
		},
		{
			name:      "ls_command",
			toolName:  "Bash",
			inputStr:  `{"command": "ls -la /some/directory"}`,
			outputStr: "total 123\ndrwxr-xr-x some long listing that is at least 50 chars",
			expected:  true,
		},
		// Valid operations that should NOT be skipped
		{
			name:      "valid_read",
			toolName:  "Read",
			inputStr:  `{"file_path": "/project/main.go"}`,
			outputStr: "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"Hello World\")\n}",
			expected:  false,
		},
		{
			name:      "valid_edit",
			toolName:  "Edit",
			inputStr:  `{"file_path": "/project/handler.go", "old_string": "foo", "new_string": "bar"}`,
			outputStr: "Edit applied successfully. File /project/handler.go has been modified with the requested changes.",
			expected:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := shouldSkipTrivialOperation(tt.toolName, tt.inputStr, tt.outputStr)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestTruncateForLog(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxLen   int
		expected string
	}{
		{
			name:     "shorter_than_max",
			input:    "hello",
			maxLen:   10,
			expected: "hello",
		},
		{
			name:     "equal_to_max",
			input:    "hello",
			maxLen:   5,
			expected: "hello",
		},
		{
			name:     "longer_than_max",
			input:    "hello world",
			maxLen:   5,
			expected: "hello...",
		},
		{
			name:     "empty_string",
			input:    "",
			maxLen:   5,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncateForLog(tt.input, tt.maxLen)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestToJSONString(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected string
	}{
		{
			name:     "nil_value",
			input:    nil,
			expected: "",
		},
		{
			name:     "string_value",
			input:    "hello",
			expected: "hello",
		},
		{
			name:     "int_value",
			input:    42,
			expected: "42",
		},
		{
			name:     "map_value",
			input:    map[string]string{"key": "value"},
			expected: `{"key":"value"}`,
		},
		{
			name:     "slice_value",
			input:    []string{"a", "b", "c"},
			expected: `["a","b","c"]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := toJSONString(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCaptureFileMtimes(t *testing.T) {
	// Create a temp directory with test files
	tmpDir, err := os.MkdirTemp("", "mtime-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create test files
	file1 := filepath.Join(tmpDir, "file1.txt")
	file2 := filepath.Join(tmpDir, "file2.txt")

	err = os.WriteFile(file1, []byte("content1"), 0644)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(file2, []byte("content2"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("captures_mtimes_for_existing_files", func(t *testing.T) {
		mtimes := captureFileMtimes([]string{file1}, []string{file2}, "")
		assert.Len(t, mtimes, 2)
		assert.Contains(t, mtimes, file1)
		assert.Contains(t, mtimes, file2)
		assert.Greater(t, mtimes[file1], int64(0))
		assert.Greater(t, mtimes[file2], int64(0))
	})

	t.Run("handles_nonexistent_files", func(t *testing.T) {
		mtimes := captureFileMtimes([]string{"/nonexistent/file.txt"}, nil, "")
		assert.Empty(t, mtimes)
	})

	t.Run("handles_relative_paths_with_cwd", func(t *testing.T) {
		mtimes := captureFileMtimes([]string{"file1.txt"}, nil, tmpDir)
		assert.Len(t, mtimes, 1)
		assert.Contains(t, mtimes, "file1.txt")
	})

	t.Run("empty_inputs", func(t *testing.T) {
		mtimes := captureFileMtimes(nil, nil, "")
		assert.Empty(t, mtimes)
	})
}

func TestGetFileMtimes(t *testing.T) {
	// Create a temp file
	tmpDir, err := os.MkdirTemp("", "getmtime-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	testFile := filepath.Join(tmpDir, "test.txt")
	err = os.WriteFile(testFile, []byte("content"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	mtimes := GetFileMtimes([]string{testFile}, "")
	assert.Len(t, mtimes, 1)
	assert.Contains(t, mtimes, testFile)
	assert.Greater(t, mtimes[testFile], int64(0))
}

func TestGetFileContent(t *testing.T) {
	// Create a temp directory with test files
	tmpDir, err := os.MkdirTemp("", "content-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	t.Run("reads_existing_file", func(t *testing.T) {
		testFile := filepath.Join(tmpDir, "test.txt")
		content := "test content"
		err := os.WriteFile(testFile, []byte(content), 0644)
		if err != nil {
			t.Fatal(err)
		}

		result, ok := GetFileContent(testFile, "")
		assert.True(t, ok)
		assert.Equal(t, content, result)
	})

	t.Run("returns_false_for_nonexistent_file", func(t *testing.T) {
		result, ok := GetFileContent("/nonexistent/file.txt", "")
		assert.False(t, ok)
		assert.Empty(t, result)
	})

	t.Run("truncates_long_content", func(t *testing.T) {
		testFile := filepath.Join(tmpDir, "long.txt")
		longContent := ""
		for i := 0; i < 3000; i++ {
			longContent += "x"
		}
		err := os.WriteFile(testFile, []byte(longContent), 0644)
		if err != nil {
			t.Fatal(err)
		}

		result, ok := GetFileContent(testFile, "")
		assert.True(t, ok)
		assert.Contains(t, result, "[truncated]")
		assert.LessOrEqual(t, len(result), 2100)
	})

	t.Run("resolves_relative_path_with_cwd", func(t *testing.T) {
		testFile := filepath.Join(tmpDir, "relative.txt")
		content := "relative content"
		err := os.WriteFile(testFile, []byte(content), 0644)
		if err != nil {
			t.Fatal(err)
		}

		result, ok := GetFileContent("relative.txt", tmpDir)
		assert.True(t, ok)
		assert.Equal(t, content, result)
	})
}

func TestMaxConcurrentCLICalls(t *testing.T) {
	assert.Equal(t, 4, MaxConcurrentCLICalls)
}

func TestObservationTypes(t *testing.T) {
	expected := []string{"bugfix", "feature", "refactor", "change", "discovery", "decision"}
	assert.Equal(t, expected, ObservationTypes)
}

func TestObservationConcepts(t *testing.T) {
	expectedConcepts := []string{
		"how-it-works",
		"why-it-exists",
		"what-changed",
		"problem-solution",
		"gotcha",
		"pattern",
		"trade-off",
	}
	assert.Equal(t, expectedConcepts, ObservationConcepts)
}
