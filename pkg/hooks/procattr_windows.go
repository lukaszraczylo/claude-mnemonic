//go:build windows

package hooks

import "os/exec"

func setSysProcAttr(cmd *exec.Cmd) {
	// Windows has no Setpgid; process groups work differently.
}
