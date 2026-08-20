package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/SilkageNet/codex-switch/internal/atomicfile"
)

type State struct {
	Version         int       `json:"version"`
	ActiveProfileID string    `json:"activeProfileId,omitempty"`
	AuthHash        string    `json:"authHash,omitempty"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

func Load(path string) (State, error) {
	data, err := atomicfile.ReadLimited(path, 1<<20)
	if errors.Is(err, os.ErrNotExist) {
		return State{Version: 1}, nil
	}
	if err != nil {
		return State{}, err
	}
	var result State
	if err := json.Unmarshal(data, &result); err != nil {
		return State{}, fmt.Errorf("decode state: %w", err)
	}
	if result.Version != 1 {
		return State{}, fmt.Errorf("unsupported state version %d", result.Version)
	}
	return result, nil
}

func Save(path string, value State) error {
	value.Version = 1
	value.UpdatedAt = time.Now().UTC()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(path, append(data, '\n'), 0o600)
}
