//go:build windows

package process

import (
	"encoding/csv"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

type Info struct {
	PID     int    `json:"pid"`
	Command string `json:"command"`
}

func DetectCodex() ([]Info, error) {
	output, err := exec.Command("tasklist", "/FO", "CSV", "/NH").Output()
	if err != nil {
		return nil, fmt.Errorf("inspect running processes: %w", err)
	}
	records, err := csv.NewReader(strings.NewReader(string(output))).ReadAll()
	if err != nil {
		return nil, fmt.Errorf("parse task list: %w", err)
	}
	var matches []Info
	for _, record := range records {
		if len(record) < 2 {
			continue
		}
		name := strings.ToLower(record[0])
		if name != "codex.exe" && name != "chatgpt.exe" {
			continue
		}
		pid, _ := strconv.Atoi(record[1])
		matches = append(matches, Info{PID: pid, Command: record[0]})
	}
	return matches, nil
}
