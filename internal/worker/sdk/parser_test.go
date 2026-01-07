package sdk

import (
	"testing"

	"github.com/lukaszraczylo/claude-mnemonic/pkg/models"
	"github.com/stretchr/testify/assert"
)

func TestParseObservations_SingleObservation(t *testing.T) {
	text := `Some text before
<observation>
<type>bugfix</type>
<title>Fixed null pointer error</title>
<subtitle>In user service</subtitle>
<narrative>The service was crashing when user ID was nil</narrative>
<facts>
<fact>Added nil check</fact>
<fact>Added unit test</fact>
</facts>
<concepts>
<concept>error-handling</concept>
<concept>debugging</concept>
</concepts>
<files_read>
<file>user_service.go</file>
</files_read>
<files_modified>
<file>user_service.go</file>
<file>user_service_test.go</file>
</files_modified>
</observation>
Some text after`

	observations := ParseObservations(text, "test-correlation-id")

	assert.Len(t, observations, 1)
	obs := observations[0]
	assert.Equal(t, models.ObservationType("bugfix"), obs.Type)
	assert.Equal(t, "Fixed null pointer error", obs.Title)
	assert.Equal(t, "In user service", obs.Subtitle)
	assert.Equal(t, "The service was crashing when user ID was nil", obs.Narrative)
	assert.Equal(t, []string{"Added nil check", "Added unit test"}, obs.Facts)
	assert.Equal(t, []string{"error-handling", "debugging"}, obs.Concepts)
	assert.Equal(t, []string{"user_service.go"}, obs.FilesRead)
	assert.Equal(t, []string{"user_service.go", "user_service_test.go"}, obs.FilesModified)
}

func TestParseObservations_MultipleObservations(t *testing.T) {
	text := `
<observation>
<type>feature</type>
<title>Added caching</title>
<narrative>Implemented Redis caching</narrative>
<facts><fact>Added cache layer</fact></facts>
<concepts><concept>caching</concept></concepts>
</observation>
<observation>
<type>refactor</type>
<title>Cleaned up code</title>
<narrative>Removed dead code</narrative>
<facts><fact>Removed unused functions</fact></facts>
<concepts><concept>refactoring</concept></concepts>
</observation>
`

	observations := ParseObservations(text, "test-id")

	assert.Len(t, observations, 2)
	assert.Equal(t, models.ObservationType("feature"), observations[0].Type)
	assert.Equal(t, "Added caching", observations[0].Title)
	assert.Equal(t, models.ObservationType("refactor"), observations[1].Type)
	assert.Equal(t, "Cleaned up code", observations[1].Title)
}

func TestParseObservations_TableDriven(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		expectedType  models.ObservationType
		expectedTitle string
		checkConcepts []string
		expectedCount int
	}{
		{
			name: "valid_bugfix_observation",
			input: `<observation>
<type>bugfix</type>
<title>Fixed bug</title>
<narrative>Details</narrative>
</observation>`,
			expectedCount: 1,
			expectedType:  models.ObsTypeBugfix,
			expectedTitle: "Fixed bug",
		},
		{
			name: "valid_feature_observation",
			input: `<observation>
<type>feature</type>
<title>New feature</title>
<narrative>Added new stuff</narrative>
</observation>`,
			expectedCount: 1,
			expectedType:  models.ObsTypeFeature,
			expectedTitle: "New feature",
		},
		{
			name: "valid_refactor_observation",
			input: `<observation>
<type>refactor</type>
<title>Code cleanup</title>
<narrative>Refactored module</narrative>
</observation>`,
			expectedCount: 1,
			expectedType:  models.ObsTypeRefactor,
			expectedTitle: "Code cleanup",
		},
		{
			name: "valid_change_observation",
			input: `<observation>
<type>change</type>
<title>Config update</title>
<narrative>Changed settings</narrative>
</observation>`,
			expectedCount: 1,
			expectedType:  models.ObsTypeChange,
			expectedTitle: "Config update",
		},
		{
			name: "valid_discovery_observation",
			input: `<observation>
<type>discovery</type>
<title>Found pattern</title>
<narrative>Discovered new pattern</narrative>
</observation>`,
			expectedCount: 1,
			expectedType:  models.ObsTypeDiscovery,
			expectedTitle: "Found pattern",
		},
		{
			name: "valid_decision_observation",
			input: `<observation>
<type>decision</type>
<title>Architecture decision</title>
<narrative>Chose microservices</narrative>
</observation>`,
			expectedCount: 1,
			expectedType:  models.ObsTypeDecision,
			expectedTitle: "Architecture decision",
		},
		{
			name: "invalid_type_defaults_to_change",
			input: `<observation>
<type>invalid_type</type>
<title>Some title</title>
<narrative>Details</narrative>
</observation>`,
			expectedCount: 1,
			expectedType:  models.ObsTypeChange,
			expectedTitle: "Some title",
		},
		{
			name: "missing_type_defaults_to_change",
			input: `<observation>
<title>No type specified</title>
<narrative>Details</narrative>
</observation>`,
			expectedCount: 1,
			expectedType:  models.ObsTypeChange,
			expectedTitle: "No type specified",
		},
		{
			name:          "empty_input",
			input:         "",
			expectedCount: 0,
		},
		{
			name:          "no_observation_tags",
			input:         "Just regular text without any observation",
			expectedCount: 0,
		},
		{
			name: "valid_concepts_filtered",
			input: `<observation>
<type>bugfix</type>
<title>Test</title>
<narrative>Test</narrative>
<concepts>
<concept>best-practice</concept>
<concept>invalid-concept</concept>
<concept>security</concept>
</concepts>
</observation>`,
			expectedCount: 1,
			expectedType:  models.ObsTypeBugfix,
			checkConcepts: []string{"best-practice", "security"},
		},
		{
			name: "type_in_concepts_filtered_out",
			input: `<observation>
<type>bugfix</type>
<title>Test</title>
<narrative>Test</narrative>
<concepts>
<concept>bugfix</concept>
<concept>security</concept>
</concepts>
</observation>`,
			expectedCount: 1,
			expectedType:  models.ObsTypeBugfix,
			checkConcepts: []string{"security"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			observations := ParseObservations(tt.input, "test-correlation-id")

			assert.Len(t, observations, tt.expectedCount)
			if tt.expectedCount > 0 {
				obs := observations[0]
				assert.Equal(t, tt.expectedType, obs.Type)
				if tt.expectedTitle != "" {
					assert.Equal(t, tt.expectedTitle, obs.Title)
				}
				if tt.checkConcepts != nil {
					assert.Equal(t, tt.checkConcepts, obs.Concepts)
				}
			}
		})
	}
}

func TestParseObservations_AllValidConcepts(t *testing.T) {
	// Test all valid concepts are accepted
	validConcepts := []string{
		"how-it-works", "why-it-exists", "what-changed", "problem-solution", "gotcha", "pattern", "trade-off",
		"best-practice", "anti-pattern", "architecture", "security", "performance", "testing", "debugging", "workflow", "tooling",
		"refactoring", "api", "database", "configuration", "error-handling", "caching", "logging", "auth", "validation",
	}

	for _, concept := range validConcepts {
		t.Run("concept_"+concept, func(t *testing.T) {
			input := `<observation>
<type>discovery</type>
<title>Test</title>
<narrative>Test</narrative>
<concepts><concept>` + concept + `</concept></concepts>
</observation>`

			observations := ParseObservations(input, "test-id")
			assert.Len(t, observations, 1)
			assert.Contains(t, observations[0].Concepts, concept)
		})
	}
}

func TestParseObservations_ConceptCaseInsensitive(t *testing.T) {
	input := `<observation>
<type>discovery</type>
<title>Test</title>
<narrative>Test</narrative>
<concepts>
<concept>SECURITY</concept>
<concept>Best-Practice</concept>
<concept>  caching  </concept>
</concepts>
</observation>`

	observations := ParseObservations(input, "test-id")

	assert.Len(t, observations, 1)
	assert.Equal(t, []string{"security", "best-practice", "caching"}, observations[0].Concepts)
}

func TestParseSummary_ValidSummary(t *testing.T) {
	text := `Some text before
<summary>
<request>User asked to fix the bug</request>
<investigated>Looked at error logs and stack traces</investigated>
<learned>The issue was a race condition</learned>
<completed>Fixed the race condition with mutex</completed>
<next_steps>Add more tests for concurrent access</next_steps>
<notes>May need to review similar code elsewhere</notes>
</summary>
Some text after`

	summary := ParseSummary(text, 123)

	assert.NotNil(t, summary)
	assert.Equal(t, "User asked to fix the bug", summary.Request)
	assert.Equal(t, "Looked at error logs and stack traces", summary.Investigated)
	assert.Equal(t, "The issue was a race condition", summary.Learned)
	assert.Equal(t, "Fixed the race condition with mutex", summary.Completed)
	assert.Equal(t, "Add more tests for concurrent access", summary.NextSteps)
	assert.Equal(t, "May need to review similar code elsewhere", summary.Notes)
}

func TestParseSummary_TableDriven(t *testing.T) {
	tests := []struct {
		name            string
		input           string
		expectedRequest string
		sessionID       int64
		expectNil       bool
	}{
		{
			name:      "empty_input",
			input:     "",
			sessionID: 1,
			expectNil: true,
		},
		{
			name:      "no_summary_tag",
			input:     "Just some text without summary",
			sessionID: 1,
			expectNil: true,
		},
		{
			name:      "skip_summary_tag",
			input:     `<skip_summary reason="No significant changes made"/>`,
			sessionID: 1,
			expectNil: true,
		},
		{
			name:      "skip_summary_with_different_reason",
			input:     `<skip_summary reason="Only read files"/>`,
			sessionID: 2,
			expectNil: true,
		},
		{
			name: "valid_summary_minimal",
			input: `<summary>
<request>Test request</request>
</summary>`,
			sessionID:       3,
			expectNil:       false,
			expectedRequest: "Test request",
		},
		{
			name: "valid_summary_all_fields",
			input: `<summary>
<request>Full request</request>
<investigated>Full investigated</investigated>
<learned>Full learned</learned>
<completed>Full completed</completed>
<next_steps>Full next steps</next_steps>
<notes>Full notes</notes>
</summary>`,
			sessionID:       4,
			expectNil:       false,
			expectedRequest: "Full request",
		},
		{
			name: "summary_with_empty_fields",
			input: `<summary>
<request></request>
<investigated></investigated>
</summary>`,
			sessionID:       5,
			expectNil:       false,
			expectedRequest: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			summary := ParseSummary(tt.input, tt.sessionID)

			if tt.expectNil {
				assert.Nil(t, summary)
			} else {
				assert.NotNil(t, summary)
				assert.Equal(t, tt.expectedRequest, summary.Request)
			}
		})
	}
}

func TestParseSummary_SkipSummaryPriority(t *testing.T) {
	// skip_summary should take priority over summary block
	text := `<skip_summary reason="No changes"/>
<summary>
<request>This should be ignored</request>
</summary>`

	summary := ParseSummary(text, 1)
	assert.Nil(t, summary)
}

func TestExtractField_TableDriven(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		fieldName string
		expected  string
	}{
		{
			name:      "simple_field",
			content:   "<title>Test Title</title>",
			fieldName: "title",
			expected:  "Test Title",
		},
		{
			name:      "field_with_whitespace",
			content:   "<title>  Test Title  </title>",
			fieldName: "title",
			expected:  "Test Title",
		},
		{
			name:      "field_not_found",
			content:   "<other>Value</other>",
			fieldName: "title",
			expected:  "",
		},
		{
			name:      "empty_field",
			content:   "<title></title>",
			fieldName: "title",
			expected:  "",
		},
		{
			name:      "nested_content",
			content:   "<wrapper><title>Nested</title></wrapper>",
			fieldName: "title",
			expected:  "Nested",
		},
		{
			name:      "field_among_others",
			content:   "<a>A</a><title>Target</title><b>B</b>",
			fieldName: "title",
			expected:  "Target",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractField(tt.content, tt.fieldName)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractArrayElements_TableDriven(t *testing.T) {
	tests := []struct {
		name        string
		content     string
		arrayName   string
		elementName string
		expected    []string
	}{
		{
			name:        "simple_array",
			content:     "<facts><fact>One</fact><fact>Two</fact></facts>",
			arrayName:   "facts",
			elementName: "fact",
			expected:    []string{"One", "Two"},
		},
		{
			name:        "empty_array",
			content:     "<facts></facts>",
			arrayName:   "facts",
			elementName: "fact",
			expected:    nil,
		},
		{
			name:        "array_not_found",
			content:     "<other><item>Value</item></other>",
			arrayName:   "facts",
			elementName: "fact",
			expected:    nil,
		},
		{
			name:        "single_element",
			content:     "<concepts><concept>security</concept></concepts>",
			arrayName:   "concepts",
			elementName: "concept",
			expected:    []string{"security"},
		},
		{
			name: "multiline_array",
			content: `<files>
<file>file1.go</file>
<file>file2.go</file>
<file>file3.go</file>
</files>`,
			arrayName:   "files",
			elementName: "file",
			expected:    []string{"file1.go", "file2.go", "file3.go"},
		},
		{
			name:        "whitespace_trimmed",
			content:     "<items><item>  trimmed  </item></items>",
			arrayName:   "items",
			elementName: "item",
			expected:    []string{"trimmed"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractArrayElements(tt.content, tt.arrayName, tt.elementName)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestValidObsTypes(t *testing.T) {
	expected := map[string]bool{
		"bugfix":    true,
		"feature":   true,
		"refactor":  true,
		"change":    true,
		"discovery": true,
		"decision":  true,
	}
	assert.Equal(t, expected, validObsTypes)
}

func TestValidConcepts(t *testing.T) {
	// Verify expected concepts are valid
	expectedValid := []string{
		"how-it-works", "why-it-exists", "what-changed", "problem-solution", "gotcha", "pattern", "trade-off",
		"best-practice", "anti-pattern", "architecture", "security", "performance", "testing", "debugging", "workflow", "tooling",
		"refactoring", "api", "database", "configuration", "error-handling", "caching", "logging", "auth", "validation",
	}

	for _, concept := range expectedValid {
		assert.True(t, validConcepts[concept], "Expected %s to be valid", concept)
	}

	// Verify invalid concepts
	invalidConcepts := []string{"random", "invalid", "not-a-concept", "foo", "bar"}
	for _, concept := range invalidConcepts {
		assert.False(t, validConcepts[concept], "Expected %s to be invalid", concept)
	}
}
