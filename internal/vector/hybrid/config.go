package hybrid

import (
	"os"
	"strconv"

	"github.com/rs/zerolog/log"
)

// GetStrategyFromEnv reads CLAUDE_MNEMONIC_VECTOR_STRATEGY from environment
func GetStrategyFromEnv() VectorStorageStrategy {
	strategyStr := os.Getenv("CLAUDE_MNEMONIC_VECTOR_STRATEGY")
	if strategyStr == "" {
		// Default to hub strategy for optimal balance
		return StorageHub
	}

	strategy := ParseStrategy(strategyStr)
	log.Info().
		Str("env_value", strategyStr).
		Str("strategy", strategyToString(strategy)).
		Msg("Vector storage strategy from environment")

	return strategy
}

// GetHubThresholdFromEnv reads CLAUDE_MNEMONIC_HUB_THRESHOLD from environment
func GetHubThresholdFromEnv() int {
	thresholdStr := os.Getenv("CLAUDE_MNEMONIC_HUB_THRESHOLD")
	if thresholdStr == "" {
		return 5 // Default threshold
	}

	threshold, err := strconv.Atoi(thresholdStr)
	if err != nil {
		log.Warn().
			Err(err).
			Str("env_value", thresholdStr).
			Msg("Invalid hub threshold in environment, using default")
		return 5
	}

	if threshold < 1 {
		log.Warn().
			Int("env_value", threshold).
			Msg("Hub threshold too low, using minimum of 1")
		return 1
	}

	log.Info().
		Int("threshold", threshold).
		Msg("Hub threshold from environment")

	return threshold
}

// IsHybridEnabled checks if hybrid storage should be used
// Returns false if CLAUDE_MNEMONIC_VECTOR_STRATEGY=always (backwards compat)
func IsHybridEnabled() bool {
	strategy := GetStrategyFromEnv()
	return strategy != StorageAlways
}
