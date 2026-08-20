//go:build linux

package launcher

import (
	"fmt"
	"os/exec"
)

func Codex() error {
	for _, candidate := range [][]string{{"gtk-launch", "chatgpt"}, {"gtk-launch", "com.openai.ChatGPT"}} {
		if _, err := exec.LookPath(candidate[0]); err == nil {
			if err := exec.Command(candidate[0], candidate[1:]...).Start(); err == nil {
				return nil
			}
		}
	}
	return fmt.Errorf("could not locate a ChatGPT desktop launcher")
}
