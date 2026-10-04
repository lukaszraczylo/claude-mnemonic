package mcp

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestIdleTimeoutFromEnv(t *testing.T) {
	// The idle shutdown is off unless asked for; anything that is not a positive duration leaves it off.
	for value, want := range map[string]time.Duration{
		"":     0,
		"0":    0,
		"abc":  0,
		"-5m":  0,
		"45":   0, // a bare number is not a duration
		"90m":  90 * time.Minute,
		"2h":   2 * time.Hour,
		"30s":  30 * time.Second,
		" 2h ": 0, // not trimmed: the operator wrote it, so it is wrong rather than guessed
	} {
		t.Run(value, func(t *testing.T) {
			t.Setenv(IdleTimeoutEnv, value)
			assert.Equal(t, want, idleTimeoutFromEnv())
		})
	}
}

func TestNewServer_HasNoIdleTimeoutByDefault(t *testing.T) {
	t.Setenv(IdleTimeoutEnv, "")
	assert.Zero(t, NewServer(nil, "http://localhost:1", "p", "v").idleTimeout)
}

func TestNewServer_TakesTheIdleTimeoutFromTheEnvironment(t *testing.T) {
	t.Setenv(IdleTimeoutEnv, "3h")
	assert.Equal(t, 3*time.Hour, NewServer(nil, "http://localhost:1", "p", "v").idleTimeout)
}
