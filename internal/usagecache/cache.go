package usagecache

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/SilkageNet/codex-switch/internal/atomicfile"
	"github.com/SilkageNet/codex-switch/internal/codexusage"
)

type Cache struct {
	Version  int                            `json:"version"`
	Profiles map[string]codexusage.Snapshot `json:"profiles"`
}

func Load(path string) (Cache, error) {
	data, err := atomicfile.ReadLimited(path, 16<<20)
	if errors.Is(err, os.ErrNotExist) {
		return Cache{Version: 1, Profiles: map[string]codexusage.Snapshot{}}, nil
	}
	if err != nil {
		return Cache{}, err
	}
	var cache Cache
	if err := json.Unmarshal(data, &cache); err != nil {
		return Cache{}, fmt.Errorf("decode usage cache: %w", err)
	}
	if cache.Version != 1 {
		return Cache{}, fmt.Errorf("unsupported usage cache version %d", cache.Version)
	}
	if cache.Profiles == nil {
		cache.Profiles = map[string]codexusage.Snapshot{}
	}
	return cache, nil
}

func Save(path string, cache Cache) error {
	cache.Version = 1
	if cache.Profiles == nil {
		cache.Profiles = map[string]codexusage.Snapshot{}
	}
	data, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	return atomicfile.Write(path, append(data, '\n'), 0o600)
}
