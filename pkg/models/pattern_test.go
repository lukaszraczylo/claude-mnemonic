package models

import (
	"database/sql"
	"encoding/json"
	"testing"
	"time"
)

func TestNewPattern(t *testing.T) {
	pattern := NewPattern(
		"Test Pattern",
		PatternTypeBug,
		"A test pattern description",
		[]string{"nil", "error", "handling"},
		"test-project",
		123,
	)

	if pattern.Name != "Test Pattern" {
		t.Errorf("Expected name 'Test Pattern', got '%s'", pattern.Name)
	}
	if pattern.Type != PatternTypeBug {
		t.Errorf("Expected type PatternTypeBug, got '%s'", pattern.Type)
	}
	if !pattern.Description.Valid || pattern.Description.String != "A test pattern description" {
		t.Errorf("Description not set correctly")
	}
	if len(pattern.Signature) != 3 {
		t.Errorf("Expected 3 signature elements, got %d", len(pattern.Signature))
	}
	if pattern.Frequency != 1 {
		t.Errorf("Expected frequency 1, got %d", pattern.Frequency)
	}
	if len(pattern.Projects) != 1 || pattern.Projects[0] != "test-project" {
		t.Errorf("Projects not set correctly")
	}
	if len(pattern.ObservationIDs) != 1 || pattern.ObservationIDs[0] != 123 {
		t.Errorf("ObservationIDs not set correctly")
	}
	if pattern.Status != PatternStatusActive {
		t.Errorf("Expected status Active, got '%s'", pattern.Status)
	}
	if pattern.Confidence != 0.5 {
		t.Errorf("Expected initial confidence 0.5, got %f", pattern.Confidence)
	}
}

func TestPattern_AddOccurrence(t *testing.T) {
	pattern := NewPattern("Test", PatternTypeBug, "desc", []string{"test"}, "project1", 1)

	// Add same project occurrence
	pattern.AddOccurrence("project1", 2)
	if pattern.Frequency != 2 {
		t.Errorf("Expected frequency 2, got %d", pattern.Frequency)
	}
	if len(pattern.Projects) != 1 {
		t.Errorf("Expected 1 project (no duplicates), got %d", len(pattern.Projects))
	}

	// Add different project occurrence
	pattern.AddOccurrence("project2", 3)
	if pattern.Frequency != 3 {
		t.Errorf("Expected frequency 3, got %d", pattern.Frequency)
	}
	if len(pattern.Projects) != 2 {
		t.Errorf("Expected 2 projects, got %d", len(pattern.Projects))
	}

	// Add duplicate observation ID - should not duplicate
	pattern.AddOccurrence("project2", 3)
	if len(pattern.ObservationIDs) != 3 {
		t.Errorf("Expected 3 observation IDs (no duplicate), got %d", len(pattern.ObservationIDs))
	}

	// Check confidence increased
	if pattern.Confidence <= 0.5 {
		t.Errorf("Expected confidence to increase above 0.5, got %f", pattern.Confidence)
	}
}

func TestPattern_ConfidenceCalculation(t *testing.T) {
	tests := []struct {
		name          string
		frequency     int
		projectCount  int
		minConfidence float64
		maxConfidence float64
	}{
		{"low_frequency", 2, 1, 0.3, 0.5},
		{"high_frequency", 10, 1, 0.6, 0.8},
		{"multi_project", 3, 3, 0.4, 0.7},
		{"high_freq_multi_proj", 10, 5, 0.7, 1.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pattern := NewPattern("Test", PatternTypeBug, "", []string{}, "proj1", 1)

			// Simulate occurrences
			for i := 1; i < tt.frequency; i++ {
				projIdx := i % tt.projectCount
				if projIdx == 0 {
					projIdx = 1
				}
				pattern.AddOccurrence("proj"+string(rune('0'+projIdx)), int64(i+1))
			}

			if pattern.Confidence < tt.minConfidence || pattern.Confidence > tt.maxConfidence {
				t.Errorf("Expected confidence between %f and %f, got %f",
					tt.minConfidence, tt.maxConfidence, pattern.Confidence)
			}
		})
	}
}

func TestPatternType_Detection(t *testing.T) {
	tests := []struct {
		concepts  []string
		title     string
		narrative string
		expected  PatternType
	}{
		{[]string{"anti-pattern"}, "", "", PatternTypeAntiPattern},
		{[]string{"best-practice"}, "", "", PatternTypeBestPractice},
		{[]string{"architecture"}, "", "", PatternTypeArchitecture},
		{[]string{"refactor"}, "", "", PatternTypeRefactor},
		{[]string{}, "nil pointer bug", "", PatternTypeBug},
		{[]string{}, "Deadlock in concurrent code", "", PatternTypeBug},
		{[]string{}, "Extract interface", "", PatternTypeRefactor},
	}

	for _, tt := range tests {
		t.Run(tt.title+"_"+tt.expected.String(), func(t *testing.T) {
			result := DetectPatternType(tt.concepts, tt.title, tt.narrative)
			if result != tt.expected {
				t.Errorf("Expected %s, got %s", tt.expected, result)
			}
		})
	}
}

func (pt PatternType) String() string {
	return string(pt)
}

func TestExtractSignature(t *testing.T) {
	concepts := []string{"error-handling", "security"}
	title := "Nil Pointer Validation Pattern"
	narrative := "Always validate before dereferencing"

	signature := ExtractSignature(concepts, title, narrative)

	// Should contain concepts
	found := false
	for _, s := range signature {
		if s == "error-handling" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected signature to contain concepts, got %v", signature)
	}

	// Should contain significant words from title
	found = false
	for _, s := range signature {
		if s == "validation" || s == "pattern" || s == "pointer" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected signature to contain title keywords, got %v", signature)
	}
}

func TestCalculateMatchScore(t *testing.T) {
	tests := []struct {
		name     string
		sig1     []string
		sig2     []string
		minScore float64
		maxScore float64
	}{
		{"identical", []string{"a", "b", "c"}, []string{"a", "b", "c"}, 1.0, 1.0},
		{"partial", []string{"a", "b", "c"}, []string{"a", "b", "d"}, 0.4, 0.6},
		{"no_match", []string{"a", "b", "c"}, []string{"x", "y", "z"}, 0.0, 0.0},
		{"empty", []string{}, []string{"a", "b"}, 0.0, 0.0},
		{"subset", []string{"a", "b"}, []string{"a", "b", "c", "d"}, 0.4, 0.6},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := CalculateMatchScore(tt.sig1, tt.sig2)
			if score < tt.minScore || score > tt.maxScore {
				t.Errorf("Expected score between %f and %f, got %f",
					tt.minScore, tt.maxScore, score)
			}
		})
	}
}

func TestPattern_MarshalJSON(t *testing.T) {
	pattern := &Pattern{
		ID:             1,
		Name:           "Test Pattern",
		Type:           PatternTypeBug,
		Description:    sql.NullString{String: "A description", Valid: true},
		Signature:      []string{"a", "b"},
		Recommendation: sql.NullString{String: "Do this", Valid: true},
		Frequency:      5,
		Projects:       []string{"proj1", "proj2"},
		ObservationIDs: []int64{1, 2, 3},
		Status:         PatternStatusActive,
		MergedIntoID:   sql.NullInt64{Int64: 0, Valid: false},
		Confidence:     0.8,
		LastSeenAt:     time.Now().Format(time.RFC3339),
		LastSeenEpoch:  time.Now().UnixMilli(),
		CreatedAt:      time.Now().Format(time.RFC3339),
		CreatedAtEpoch: time.Now().UnixMilli(),
	}

	data, err := json.Marshal(pattern)
	if err != nil {
		t.Fatalf("Failed to marshal pattern: %v", err)
	}

	var result PatternJSON
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("Failed to unmarshal pattern: %v", err)
	}

	if result.Name != pattern.Name {
		t.Errorf("Expected name %s, got %s", pattern.Name, result.Name)
	}
	if result.Description != pattern.Description.String {
		t.Errorf("Expected description %s, got %s", pattern.Description.String, result.Description)
	}
	if result.Frequency != pattern.Frequency {
		t.Errorf("Expected frequency %d, got %d", pattern.Frequency, result.Frequency)
	}
	if result.MergedIntoID != 0 {
		t.Errorf("Expected merged_into_id 0 for invalid NullInt64, got %d", result.MergedIntoID)
	}
}

func TestJSONInt64Array_Scan(t *testing.T) {
	tests := []struct {
		name     string
		input    interface{}
		expected JSONInt64Array
		wantErr  bool
	}{
		{"string_array", "[1, 2, 3]", JSONInt64Array{1, 2, 3}, false},
		{"bytes_array", []byte("[4, 5, 6]"), JSONInt64Array{4, 5, 6}, false},
		{"nil", nil, nil, false},
		{"empty_string", "", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var arr JSONInt64Array
			err := arr.Scan(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("Scan() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if len(arr) != len(tt.expected) {
				t.Errorf("Expected length %d, got %d", len(tt.expected), len(arr))
			}
		})
	}
}

func TestJSONInt64Array_Value(t *testing.T) {
	arr := JSONInt64Array{1, 2, 3}
	val, err := arr.Value()
	if err != nil {
		t.Fatalf("Value() error = %v", err)
	}

	bytes, ok := val.([]byte)
	if !ok {
		t.Fatalf("Expected []byte, got %T", val)
	}

	var result []int64
	if err := json.Unmarshal(bytes, &result); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if len(result) != 3 || result[0] != 1 || result[1] != 2 || result[2] != 3 {
		t.Errorf("Expected [1, 2, 3], got %v", result)
	}
}

func TestPatternSignatureKeywords(t *testing.T) {
	// Verify keywords exist for each type
	types := []PatternType{
		PatternTypeBug,
		PatternTypeRefactor,
		PatternTypeArchitecture,
		PatternTypeAntiPattern,
		PatternTypeBestPractice,
	}

	for _, pt := range types {
		keywords := PatternSignatureKeywords[pt]
		if len(keywords) == 0 {
			t.Errorf("No keywords defined for pattern type %s", pt)
		}
	}
}

func TestUniqueStrings(t *testing.T) {
	tests := []struct {
		input    []string
		expected int
	}{
		{[]string{"a", "b", "c"}, 3},
		{[]string{"a", "a", "b"}, 2},
		{[]string{"a", "a", "a"}, 1},
		{[]string{}, 0},
	}

	for _, tt := range tests {
		result := uniqueStrings(tt.input)
		if len(result) != tt.expected {
			t.Errorf("uniqueStrings(%v) = %v (len=%d), expected len=%d",
				tt.input, result, len(result), tt.expected)
		}
	}
}

func TestContainsIgnoreCase(t *testing.T) {
	tests := []struct {
		text     string
		substr   string
		expected bool
	}{
		{"Hello World", "hello", true},
		{"Hello World", "WORLD", true},
		{"Hello World", "xyz", false},
		{"", "a", false},
		{"a", "", true},
	}

	for _, tt := range tests {
		result := containsIgnoreCase(tt.text, tt.substr)
		if result != tt.expected {
			t.Errorf("containsIgnoreCase(%q, %q) = %v, expected %v",
				tt.text, tt.substr, result, tt.expected)
		}
	}
}
