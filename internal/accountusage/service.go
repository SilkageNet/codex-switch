package accountusage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/SilkageNet/codex-switch/internal/atomicfile"
	"github.com/SilkageNet/codex-switch/internal/authschema"
	"github.com/SilkageNet/codex-switch/internal/codexhome"
	"github.com/SilkageNet/codex-switch/internal/codexusage"
	appconfig "github.com/SilkageNet/codex-switch/internal/config"
	"github.com/SilkageNet/codex-switch/internal/filelock"
	appstate "github.com/SilkageNet/codex-switch/internal/state"
	"github.com/SilkageNet/codex-switch/internal/usagecache"
	"github.com/SilkageNet/codex-switch/internal/vault"
)

const queryTimeout = 20 * time.Second

type QueryRunner interface {
	Query(context.Context, json.RawMessage) (codexusage.Snapshot, json.RawMessage, error)
}

type Service struct {
	Home   codexhome.Home
	Paths  appconfig.Paths
	Vault  *vault.Manager
	Runner QueryRunner
}

type Result struct {
	ProfileID string              `json:"profileId"`
	Snapshot  codexusage.Snapshot `json:"snapshot,omitempty"`
	Error     string              `json:"error,omitempty"`
}

type queryResult struct {
	profileID string
	snapshot  codexusage.Snapshot
	auth      json.RawMessage
	err       error
}

func (service Service) Cached() (usagecache.Cache, error) {
	return usagecache.Load(service.Paths.UsageCache)
}

func (service Service) Refresh(ctx context.Context, profileIDs []string) (map[string]Result, error) {
	if service.Runner == nil {
		return nil, errors.New("account usage runner is not configured")
	}
	lock, err := filelock.Acquire(filepath.Join(service.Home.Path, ".codex-switch.lock"))
	if err != nil {
		return nil, err
	}
	defer func() { _ = lock.Close() }()

	data, err := service.Vault.Load()
	if err != nil {
		return nil, err
	}
	selected := make([]vault.Profile, 0, len(profileIDs))
	seen := make(map[string]bool, len(profileIDs))
	for _, id := range profileIDs {
		profile, findErr := data.Find(id)
		if findErr != nil {
			return nil, findErr
		}
		if !seen[profile.ID] {
			selected = append(selected, *profile)
			seen[profile.ID] = true
		}
	}

	queried := make(chan queryResult, len(selected))
	semaphore := make(chan struct{}, 4)
	var group sync.WaitGroup
	for _, profile := range selected {
		profile := profile
		group.Add(1)
		go func() {
			defer group.Done()
			select {
			case semaphore <- struct{}{}:
				defer func() { <-semaphore }()
			case <-ctx.Done():
				queried <- queryResult{profileID: profile.ID, err: ctx.Err()}
				return
			}
			queryContext, cancel := context.WithTimeout(ctx, queryTimeout)
			defer cancel()
			snapshot, updatedAuth, queryErr := service.Runner.Query(queryContext, profile.Auth)
			queried <- queryResult{profileID: profile.ID, snapshot: snapshot, auth: updatedAuth, err: queryErr}
		}()
	}
	group.Wait()
	close(queried)

	results := make(map[string]Result, len(selected))
	candidates := make(map[string]json.RawMessage, len(selected))
	cache, err := usagecache.Load(service.Paths.UsageCache)
	if err != nil {
		return nil, err
	}
	for result := range queried {
		entry := Result{ProfileID: result.profileID}
		if result.err != nil {
			entry.Error = result.err.Error()
		} else {
			entry.Snapshot = result.snapshot
			candidates[result.profileID] = result.auth
		}
		results[result.profileID] = entry
	}

	data, err = service.Vault.Load()
	if err != nil {
		return nil, err
	}
	state, stateErr := appstate.Load(service.Paths.State)
	if stateErr != nil && !errors.Is(stateErr, os.ErrNotExist) {
		return nil, stateErr
	}
	vaultChanged := false
	for profileID, candidateRaw := range candidates {
		profile, findErr := data.Find(profileID)
		if findErr != nil {
			entry := results[profileID]
			entry.Error = appendError(entry.Error, "profile was removed while usage was being queried")
			results[profileID] = entry
			delete(cache.Profiles, profileID)
			continue
		}
		candidate, parseErr := authschema.Parse(candidateRaw)
		if parseErr != nil {
			entry := results[profileID]
			entry.Error = appendError(entry.Error, "refreshed credentials are invalid")
			results[profileID] = entry
			continue
		}
		if candidate.Tokens.AccountID != profile.AccountID || (profile.WorkspaceID != "" && candidate.WorkspaceID != "" && candidate.WorkspaceID != profile.WorkspaceID) {
			entry := results[profileID]
			entry.Error = appendError(entry.Error, "refreshed credentials do not match the saved account")
			results[profileID] = entry
			continue
		}
		cache.Profiles[profileID] = results[profileID].Snapshot
		saved, parseErr := authschema.Parse(profile.Auth)
		if parseErr != nil {
			entry := results[profileID]
			entry.Error = appendError(entry.Error, "saved credentials became invalid")
			results[profileID] = entry
			continue
		}
		decision, compareErr := authschema.CompareGeneration(saved, candidate)
		if compareErr != nil {
			entry := results[profileID]
			entry.Error = appendError(entry.Error, credentialError(compareErr))
			results[profileID] = entry
			continue
		}
		if decision != authschema.GenerationAdoptLive {
			continue
		}
		profile.Auth = append(json.RawMessage(nil), candidate.Raw...)
		profile.Email = candidate.Email
		profile.WorkspaceID = candidate.WorkspaceID
		if refreshed, ok := candidate.GenerationTime(); ok {
			profile.TokenUpdatedAt = refreshed
		}
		vaultChanged = true

		if state.ActiveProfileID == profile.ID {
			changed, syncErr := service.reconcileActive(profile, candidate, &state)
			if syncErr != nil {
				entry := results[profileID]
				entry.Error = appendError(entry.Error, syncErr.Error())
				results[profileID] = entry
			} else if changed {
				vaultChanged = true
			}
		}
	}
	if vaultChanged {
		if err := service.Vault.Save(data); err != nil {
			return nil, err
		}
	}
	validProfiles := make(map[string]bool, len(data.Profiles))
	for _, profile := range data.Profiles {
		validProfiles[profile.ID] = true
	}
	for profileID := range cache.Profiles {
		if !validProfiles[profileID] {
			delete(cache.Profiles, profileID)
		}
	}
	if err := usagecache.Save(service.Paths.UsageCache, cache); err != nil {
		return nil, err
	}
	return results, nil
}

func (service Service) reconcileActive(profile *vault.Profile, candidate authschema.Document, state *appstate.State) (bool, error) {
	liveRaw, err := service.Home.ReadAuth()
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read active credentials after refresh: %w", err)
	}
	liveHash := atomicfile.Hash(liveRaw)
	live, err := authschema.Parse(liveRaw)
	if err != nil {
		return false, fmt.Errorf("validate active credentials after refresh: %w", err)
	}
	if live.Tokens.AccountID != candidate.Tokens.AccountID {
		return false, errors.New("active account changed while usage was being queried; refreshed credentials were kept only in the vault")
	}
	decision, err := authschema.CompareGeneration(candidate, live)
	if err != nil {
		return false, errors.New(credentialError(err))
	}
	switch decision {
	case authschema.GenerationAdoptLive:
		profile.Auth = append(json.RawMessage(nil), live.Raw...)
		profile.Email = live.Email
		profile.WorkspaceID = live.WorkspaceID
		if refreshed, ok := live.GenerationTime(); ok {
			profile.TokenUpdatedAt = refreshed
		}
		return true, nil
	case authschema.GenerationUseSaved:
		currentHash, hashErr := service.Home.AuthHash()
		if hashErr != nil {
			return false, hashErr
		}
		if currentHash != liveHash {
			return false, errors.New("active credentials changed while usage was being queried; no active file was replaced")
		}
		if err := service.Home.WriteAuth(candidate.Raw); err != nil {
			return false, fmt.Errorf("publish refreshed active credentials: %w", err)
		}
		publishedHash, err := service.Home.AuthHash()
		if err != nil {
			return false, err
		}
		if err := appstate.Save(service.Paths.State, appstate.State{ActiveProfileID: profile.ID, AuthHash: publishedHash}); err != nil {
			return false, fmt.Errorf("record refreshed active credentials: %w", err)
		}
		*state = appstate.State{Version: 1, ActiveProfileID: profile.ID, AuthHash: publishedHash}
	}
	return false, nil
}

func credentialError(err error) string {
	if errors.Is(err, authschema.ErrAmbiguousGeneration) {
		return "refreshed Token generations are ambiguous; reauthenticate this profile"
	}
	return err.Error()
}

func appendError(existing, next string) string {
	if existing == "" {
		return next
	}
	return existing + "; " + next
}
