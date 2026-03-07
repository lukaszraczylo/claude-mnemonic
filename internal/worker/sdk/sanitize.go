// Package sdk provides SDK agent integration for claude-mnemonic.
package sdk

import "github.com/lukaszraczylo/claude-mnemonic/pkg/sanitize"

// StripSystemXML removes known Claude Code internal XML blocks from text.
// Delegates to the shared sanitize package.
var StripSystemXML = sanitize.StripSystemXML
