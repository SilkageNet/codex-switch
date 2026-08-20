//go:build darwin

package launcher

import "os/exec"

func Codex() error {
	return exec.Command("open", "-a", "ChatGPT").Start()
}
