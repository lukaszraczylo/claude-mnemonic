package privacy

import (
	"testing"
)

func TestContainsSecrets(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "empty string",
			input:    "",
			expected: false,
		},
		{
			name:     "normal text",
			input:    "This is just some regular text about a bug fix",
			expected: false,
		},
		{
			name:     "API key pattern",
			input:    "api_key=abc123def456ghi789jkl012mno345pqr678",
			expected: true,
		},
		{
			name:     "api-key with dash",
			input:    `api-key: "abc123def456ghi789jkl012mno"`,
			expected: true,
		},
		{
			name:     "password in config",
			input:    `password="super_secret_password_123"`,
			expected: true,
		},
		{
			name:     "OpenAI key format",
			input:    "sk-abc123def456ghi789jkl012mno345pqr678",
			expected: true,
		},
		{
			name:     "Anthropic key format",
			input:    "sk-ant-api03-abc123def456ghi789jkl012mno345",
			expected: true,
		},
		{
			name:     "GitHub PAT",
			input:    "ghp_1234567890abcdefghijklmnopqrstuvwxyz",
			expected: true,
		},
		{
			name:     "GitHub PAT new format",
			input:    "github_pat_12ABCDEFGHIJ3456789abc_defghijklmno",
			expected: true,
		},
		{
			name:     "AWS access key",
			input:    "AKIAIOSFODNN7EXAMPLE",
			expected: true,
		},
		{
			name:     "Private key header",
			input:    "-----BEGIN RSA PRIVATE KEY-----",
			expected: true,
		},
		{
			name:     "JWT token",
			input:    "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U",
			expected: true,
		},
		{
			name:     "bearer token",
			input:    "Bearer abc123def456ghi789jkl012mno345",
			expected: true,
		},
		{
			name:     "secret_key in code",
			input:    `secret_key = "my_super_secret_token_here"`,
			expected: true,
		},
		{
			name:     "short password is not detected",
			input:    `password="short"`,
			expected: false, // Too short to trigger
		},
		{
			name:     "word password in sentence",
			input:    "The password field should be validated",
			expected: false,
		},
		{
			name:     "word api in code",
			input:    "The API returns JSON data",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ContainsSecrets(tt.input)
			if result != tt.expected {
				t.Errorf("ContainsSecrets(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestRedactSecrets(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "no secrets",
			input:    "This is safe text",
			expected: "This is safe text",
		},
		{
			name:     "API key gets redacted",
			input:    "api_key=abc123def456ghi789jkl012mno345pqr678",
			expected: "api_key=[REDACTED]",
		},
		{
			name:     "OpenAI key gets redacted",
			input:    "The key is sk-abc123def456ghi789jkl012mno345pqr678",
			expected: "The key is sk-a...[REDACTED]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := RedactSecrets(tt.input)
			if result != tt.expected {
				t.Errorf("RedactSecrets(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestSanitizeObservation(t *testing.T) {
	tests := []struct {
		name      string
		narrative string
		facts     []string
		expected  bool
	}{
		{
			name:      "clean observation",
			narrative: "Fixed a bug in the login flow",
			facts:     []string{"Users can now log in", "Session management improved"},
			expected:  false,
		},
		{
			name:      "secret in narrative",
			narrative: "Set API key api_key=abc123def456ghi789jkl012mno345",
			facts:     []string{"Configuration updated"},
			expected:  true,
		},
		{
			name:      "secret in facts",
			narrative: "Updated configuration",
			facts:     []string{"Added api_key=abc123def456ghi789jkl012mno345"},
			expected:  true,
		},
		{
			name:      "empty facts",
			narrative: "Clean narrative",
			facts:     []string{},
			expected:  false,
		},
		{
			name:      "nil facts",
			narrative: "Clean narrative",
			facts:     nil,
			expected:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SanitizeObservation(tt.narrative, tt.facts)
			if result != tt.expected {
				t.Errorf("SanitizeObservation() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func BenchmarkContainsSecrets(b *testing.B) {
	text := "This is a normal piece of text that does not contain any secrets or sensitive information"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ContainsSecrets(text)
	}
}

func BenchmarkContainsSecretsWithSecret(b *testing.B) {
	text := "api_key=abc123def456ghi789jkl012mno345pqr678"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ContainsSecrets(text)
	}
}
