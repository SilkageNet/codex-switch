//go:build windows

package launcher

import "os/exec"

func Codex() error {
	return exec.Command("cmd", "/c", "start", "", "ChatGPT").Start()
}
