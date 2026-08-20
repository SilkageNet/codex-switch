package doctor

import (
	"errors"
	"fmt"
	"os"
	"runtime"

	"github.com/SilkageNet/codex-switch/internal/authschema"
	"github.com/SilkageNet/codex-switch/internal/codexhome"
	"github.com/SilkageNet/codex-switch/internal/codexlogin"
	appconfig "github.com/SilkageNet/codex-switch/internal/config"
	"github.com/SilkageNet/codex-switch/internal/process"
	"github.com/SilkageNet/codex-switch/internal/vault"
)

type Check struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

type Report struct {
	OK     bool    `json:"ok"`
	Checks []Check `json:"checks"`
}

func Run(home codexhome.Home, paths appconfig.Paths, manager *vault.Manager, codexBinary string) Report {
	report := Report{OK: true}
	add := func(name, status, message string) {
		report.Checks = append(report.Checks, Check{Name: name, Status: status, Message: message})
		if status == "error" {
			report.OK = false
		}
	}

	if info, err := os.Stat(home.Path); err == nil && info.IsDir() {
		add("codex_home", "ok", home.Path)
	} else if errors.Is(err, os.ErrNotExist) {
		add("codex_home", "warning", home.Path+" does not exist yet")
	} else {
		add("codex_home", "error", fmt.Sprintf("cannot inspect %s: %v", home.Path, err))
	}

	if mode, err := home.CredentialStore(); err != nil {
		add("credential_store", "error", err.Error())
	} else if mode != codexhome.StoreFile {
		add("credential_store", "error", fmt.Sprintf("Codex credential store is %s; run init --enable-file-store", mode))
	} else {
		add("credential_store", "ok", "file")
	}

	if raw, err := home.ReadAuth(); err == nil {
		if document, parseErr := authschema.Parse(raw); parseErr != nil {
			add("active_auth", "error", parseErr.Error())
		} else {
			add("active_auth", "ok", fmt.Sprintf("ChatGPT account %s", document.Tokens.AccountID))
		}
	} else if errors.Is(err, os.ErrNotExist) {
		add("active_auth", "warning", "no active auth.json")
	} else {
		add("active_auth", "error", err.Error())
	}

	if data, err := manager.Load(); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			add("vault", "warning", "vault is not initialized")
		} else {
			add("vault", "error", err.Error())
		}
	} else {
		add("vault", "ok", fmt.Sprintf("%d account profile(s)", len(data.Profiles)))
	}

	if codexBinary == "" {
		add("codex_cli", "error", "codex executable not found")
	} else {
		runner := codexlogin.Runner{Binary: codexBinary}
		if version, err := runner.Version(); err != nil {
			add("codex_cli", "error", err.Error())
		} else {
			add("codex_cli", "ok", version)
		}
	}

	if running, err := process.DetectCodex(); err != nil {
		add("running_processes", "warning", err.Error())
	} else if len(running) > 0 {
		add("running_processes", "warning", fmt.Sprintf("%d Codex/ChatGPT process(es) are running", len(running)))
	} else {
		add("running_processes", "ok", "none")
	}

	if _, err := os.Stat(paths.Journal); err == nil {
		add("switch_journal", "warning", "an interrupted switch journal exists; run status to recover")
	} else if errors.Is(err, os.ErrNotExist) {
		add("switch_journal", "ok", "none")
	} else {
		add("switch_journal", "error", err.Error())
	}
	add("platform", "ok", runtime.GOOS+"/"+runtime.GOARCH)
	return report
}
