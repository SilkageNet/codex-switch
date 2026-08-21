package app

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/SilkageNet/codex-switch/internal/accountusage"
	"github.com/SilkageNet/codex-switch/internal/atomicfile"
	"github.com/SilkageNet/codex-switch/internal/authschema"
	"github.com/SilkageNet/codex-switch/internal/cliupdate"
	"github.com/SilkageNet/codex-switch/internal/codexhome"
	"github.com/SilkageNet/codex-switch/internal/codexlogin"
	"github.com/SilkageNet/codex-switch/internal/codexusage"
	appconfig "github.com/SilkageNet/codex-switch/internal/config"
	"github.com/SilkageNet/codex-switch/internal/doctor"
	"github.com/SilkageNet/codex-switch/internal/launcher"
	"github.com/SilkageNet/codex-switch/internal/secretstore"
	appstate "github.com/SilkageNet/codex-switch/internal/state"
	"github.com/SilkageNet/codex-switch/internal/switcher"
	"github.com/SilkageNet/codex-switch/internal/vault"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

type Options struct {
	CodexHome string
	AppHome   string
	CodexBin  string
	JSON      bool
	Version   string
	Input     io.Reader
	Output    io.Writer
	Error     io.Writer
}

type runtimeState struct {
	home    codexhome.Home
	paths   appconfig.Paths
	manager *vault.Manager
	bin     string
	binErr  error
}

type accountView struct {
	ID              string     `json:"id"`
	Alias           string     `json:"alias"`
	AccountID       string     `json:"accountId"`
	WorkspaceID     string     `json:"workspaceId,omitempty"`
	Email           string     `json:"email,omitempty"`
	Source          string     `json:"source"`
	Active          bool       `json:"active"`
	AuthenticatedAt time.Time  `json:"authenticatedAt"`
	LastUsedAt      time.Time  `json:"lastUsedAt,omitempty"`
	Usage           *usageView `json:"usage,omitempty"`
}

type usageView struct {
	Status     string                 `json:"status"`
	FetchedAt  time.Time              `json:"fetchedAt,omitempty"`
	PlanType   string                 `json:"planType,omitempty"`
	RateLimits *codexusage.RateLimits `json:"rateLimits,omitempty"`
	TokenUsage *codexusage.TokenUsage `json:"tokenUsage,omitempty"`
	Partial    []string               `json:"partial,omitempty"`
	Error      string                 `json:"error,omitempty"`
}

func NewCommand(version string) *cobra.Command {
	options := &Options{Version: version, Input: os.Stdin, Output: os.Stdout, Error: os.Stderr}
	root := &cobra.Command{
		Use:           "codex-switch",
		Short:         "Securely switch ChatGPT accounts in one shared CODEX_HOME",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.SetIn(options.Input)
	root.SetOut(options.Output)
	root.SetErr(options.Error)
	root.PersistentFlags().StringVar(&options.CodexHome, "codex-home", "", "Codex home (defaults to CODEX_HOME or ~/.codex)")
	root.PersistentFlags().StringVar(&options.AppHome, "home", "", "codex-switch data directory")
	root.PersistentFlags().StringVar(&options.CodexBin, "codex-binary", "", "path to the official codex executable")
	root.PersistentFlags().BoolVar(&options.JSON, "json", false, "emit machine-readable JSON without secrets")

	root.AddCommand(
		newInitCommand(options),
		newCurrentCommand(options),
		newStatusCommand(options),
		newDoctorCommand(options),
		newUseCommand(options),
		newDeactivateCommand(options),
		newSelectCommand(options),
		newAccountCommand(options),
		newVaultCommand(options),
		newUpdateCommand(options),
		newVersionCommand(options),
		newCompletionCommand(root),
	)
	return root
}

func newInitCommand(options *Options) *cobra.Command {
	var enableFileStore bool
	command := &cobra.Command{
		Use:   "init",
		Short: "Initialize the encrypted account vault",
		RunE: func(*cobra.Command, []string) error {
			runtime, err := options.loadRuntime(true)
			if err != nil {
				return err
			}
			mode, err := runtime.home.CredentialStore()
			if err != nil {
				return err
			}
			backup := ""
			if mode != codexhome.StoreFile {
				if !enableFileStore {
					return fmt.Errorf("codex credential store is %s; rerun with --enable-file-store to enable safe account projection", mode)
				}
				backup, err = runtime.home.EnableFileStore()
				if err != nil {
					return err
				}
			}
			result := map[string]any{"initialized": true, "codexHome": runtime.home.Path, "vault": runtime.paths.Vault, "credentialStore": "file"}
			if backup != "" {
				result["configBackup"] = backup
			}
			return options.render(result, fmt.Sprintf("Initialized codex-switch for %s", runtime.home.Path))
		},
	}
	command.Flags().BoolVar(&enableFileStore, "enable-file-store", false, "set cli_auth_credentials_store=file with a backup")
	return command
}

func newAccountCommand(options *Options) *cobra.Command {
	command := &cobra.Command{Use: "account", Aliases: []string{"accounts"}, Short: "Manage ChatGPT account profiles"}
	command.AddCommand(
		newAccountImportCommand(options),
		newAccountAddCommand(options, false),
		newAccountListCommand(options),
		newAccountUsageCommand(options),
		newAccountShowCommand(options),
		newAccountRenameCommand(options),
		newAccountRemoveCommand(options),
		newAccountAddCommand(options, true),
	)
	return command
}

func newAccountImportCommand(options *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "import-current <alias>",
		Short: "Import the account currently active in Codex",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			runtime, err := options.loadRuntime(true)
			if err != nil {
				return err
			}
			raw, err := runtime.home.ReadAuth()
			if err != nil {
				return fmt.Errorf("read active Codex login: %w", err)
			}
			document, err := authschema.Parse(raw)
			if err != nil {
				return err
			}
			profile, err := addDocument(runtime.manager, args[0], "import-current", document, false)
			if err != nil {
				return err
			}
			hash, err := runtime.home.AuthHash()
			if err == nil {
				_ = appstate.Save(runtime.paths.State, appstate.State{ActiveProfileID: profile.ID, AuthHash: hash})
			}
			return options.render(toView(profile, true), fmt.Sprintf("Imported current account as %q", profile.Alias))
		},
	}
}

func newAccountAddCommand(options *Options, reauth bool) *cobra.Command {
	var deviceAuth bool
	use := "add <alias>"
	short := "Add an account through the official Codex login flow"
	if reauth {
		use = "reauth <alias>"
		short = "Replace a profile through the official Codex login flow"
	}
	command := &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			runtime, err := options.loadRuntime(true)
			if err != nil {
				return err
			}
			if runtime.binErr != nil {
				return runtime.binErr
			}
			if runtime.bin == "" {
				return errors.New("official Codex executable not found; install Codex or pass --codex-binary")
			}
			runner := codexlogin.Runner{Binary: runtime.bin, Stdin: options.Input, Stdout: options.Output, Stderr: options.Error}
			document, err := runner.Login(deviceAuth)
			if err != nil {
				return err
			}
			profile, err := addDocument(runtime.manager, args[0], map[bool]string{true: "reauth", false: "codex-login"}[reauth], document, reauth)
			if err != nil {
				return err
			}
			return options.render(toView(profile, false), fmt.Sprintf("Saved account %q", profile.Alias))
		},
	}
	command.Flags().BoolVar(&deviceAuth, "device-auth", false, "use the official Codex device-code flow")
	return command
}

func newAccountListCommand(options *Options) *cobra.Command {
	var refresh bool
	var cached bool
	command := &cobra.Command{
		Use:   "list",
		Short: "List saved accounts and their usage",
		RunE: func(*cobra.Command, []string) error {
			if refresh && cached {
				return errors.New("--refresh and --cached cannot be used together")
			}
			runtime, err := options.loadRuntime(false)
			if err != nil {
				return err
			}
			data, err := runtime.manager.Load()
			if err != nil {
				return err
			}
			state, _ := appstate.Load(runtime.paths.State)
			cache, err := runtime.usageService(options.Version).Cached()
			if err != nil {
				return err
			}
			refreshIDs := make([]string, 0, len(data.Profiles))
			if !cached {
				for _, profile := range data.Profiles {
					snapshot, ok := cache.Profiles[profile.ID]
					if refresh || !ok || usageStatus(snapshot.FetchedAt, time.Now()) != "fresh" {
						refreshIDs = append(refreshIDs, profile.ID)
					}
				}
			}
			refreshErrors := map[string]string{}
			if len(refreshIDs) > 0 {
				results, refreshErr := runtime.usageService(options.Version).Refresh(context.Background(), refreshIDs)
				if refreshErr != nil {
					for _, id := range refreshIDs {
						refreshErrors[id] = refreshErr.Error()
					}
				} else {
					for id, result := range results {
						refreshErrors[id] = result.Error
					}
				}
				cache, err = runtime.usageService(options.Version).Cached()
				if err != nil {
					return err
				}
			}
			views := make([]accountView, 0, len(data.Profiles))
			for _, profile := range data.Profiles {
				view := toView(profile, profile.ID == state.ActiveProfileID)
				view.Usage = usageFromCache(cache.Profiles, profile.ID, refreshErrors[profile.ID], time.Now())
				views = append(views, view)
			}
			if options.JSON {
				return options.render(views, "")
			}
			if len(views) == 0 {
				_, _ = fmt.Fprintln(options.Output, "No accounts saved.")
				return nil
			}
			writer := tabwriter.NewWriter(options.Output, 0, 4, 2, ' ', 0)
			_, _ = fmt.Fprintln(writer, "\tALIAS\tIDENTITY\tPLAN\tLIMITS\tTOKENS\tUPDATED")
			for _, view := range views {
				marker := " "
				if view.Active {
					marker = "*"
				}
				identity := view.Email
				if identity == "" {
					identity = view.AccountID
				}
				plan, limits, tokens, updated := summarizeUsage(view.Usage, time.Now())
				_, _ = fmt.Fprintf(writer, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", marker, view.Alias, identity, plan, limits, tokens, updated)
			}
			return writer.Flush()
		},
	}
	command.Flags().BoolVar(&refresh, "refresh", false, "refresh every account before listing")
	command.Flags().BoolVar(&cached, "cached", false, "show cached usage without contacting Codex services")
	return command
}

func newAccountUsageCommand(options *Options) *cobra.Command {
	var all bool
	var cached bool
	command := &cobra.Command{
		Use:   "usage [alias]",
		Short: "Show account limits and token usage without switching",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if all && len(args) > 0 {
				return errors.New("an alias and --all cannot be used together")
			}
			runtime, err := options.loadRuntime(false)
			if err != nil {
				return err
			}
			data, err := runtime.manager.Load()
			if err != nil {
				return err
			}
			state, _ := appstate.Load(runtime.paths.State)
			profiles := make([]vault.Profile, 0, len(data.Profiles))
			switch {
			case all:
				profiles = append(profiles, data.Profiles...)
			case len(args) == 1:
				profile, findErr := data.Find(args[0])
				if findErr != nil {
					return findErr
				}
				profiles = append(profiles, *profile)
			default:
				if state.ActiveProfileID == "" {
					return errors.New("no managed account is active; pass an alias or --all")
				}
				profile, findErr := data.Find(state.ActiveProfileID)
				if findErr != nil {
					return errors.New("the active account is unmanaged; pass a saved alias")
				}
				profiles = append(profiles, *profile)
			}
			if len(profiles) == 0 {
				return errors.New("no accounts saved")
			}
			refreshErrors := map[string]string{}
			if !cached {
				ids := make([]string, 0, len(profiles))
				for _, profile := range profiles {
					ids = append(ids, profile.ID)
				}
				results, refreshErr := runtime.usageService(options.Version).Refresh(context.Background(), ids)
				if refreshErr != nil {
					if len(profiles) == 1 {
						return refreshErr
					}
					for _, id := range ids {
						refreshErrors[id] = refreshErr.Error()
					}
				} else {
					for id, result := range results {
						refreshErrors[id] = result.Error
					}
				}
			}
			cache, err := runtime.usageService(options.Version).Cached()
			if err != nil {
				return err
			}
			views := make([]accountView, 0, len(profiles))
			for _, profile := range profiles {
				view := toView(profile, profile.ID == state.ActiveProfileID)
				view.Usage = usageFromCache(cache.Profiles, profile.ID, refreshErrors[profile.ID], time.Now())
				if len(profiles) == 1 && view.Usage.Status == "unavailable" {
					if view.Usage.Error != "" {
						return errors.New(view.Usage.Error)
					}
					return errors.New("no cached usage is available; retry without --cached")
				}
				views = append(views, view)
			}
			if options.JSON {
				if len(views) == 1 {
					return options.render(views[0], "")
				}
				return options.render(views, "")
			}
			for index, view := range views {
				if index > 0 {
					_, _ = fmt.Fprintln(options.Output)
				}
				_, _ = fmt.Fprintln(options.Output, formatUsage(view, time.Now()))
			}
			return nil
		},
	}
	command.Flags().BoolVar(&all, "all", false, "show usage for every saved account")
	command.Flags().BoolVar(&cached, "cached", false, "show cached usage without contacting Codex services")
	return command
}

func newAccountShowCommand(options *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "show <alias>",
		Short: "Show non-secret account metadata",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			runtime, err := options.loadRuntime(false)
			if err != nil {
				return err
			}
			data, err := runtime.manager.Load()
			if err != nil {
				return err
			}
			profile, err := data.Find(args[0])
			if err != nil {
				return err
			}
			state, _ := appstate.Load(runtime.paths.State)
			return options.render(toView(*profile, profile.ID == state.ActiveProfileID), formatView(toView(*profile, profile.ID == state.ActiveProfileID)))
		},
	}
}

func newAccountRenameCommand(options *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "rename <old> <new>",
		Short: "Rename an account alias",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			runtime, err := options.loadRuntime(false)
			if err != nil {
				return err
			}
			data, err := runtime.manager.Load()
			if err != nil {
				return err
			}
			if err := vault.ValidateAlias(args[1]); err != nil {
				return err
			}
			profile, err := data.Find(args[0])
			if err != nil {
				return err
			}
			if existing, findErr := data.Find(args[1]); findErr == nil && existing.ID != profile.ID {
				return fmt.Errorf("account alias %q already exists", args[1])
			}
			profile.Alias = args[1]
			if err := runtime.manager.Save(data); err != nil {
				return err
			}
			return options.render(map[string]string{"old": args[0], "new": args[1]}, fmt.Sprintf("Renamed %q to %q", args[0], args[1]))
		},
	}
}

func newAccountRemoveCommand(options *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "remove <alias>",
		Short: "Remove an inactive account from the encrypted vault",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			runtime, err := options.loadRuntime(false)
			if err != nil {
				return err
			}
			data, err := runtime.manager.Load()
			if err != nil {
				return err
			}
			profile, err := data.Find(args[0])
			if err != nil {
				return err
			}
			state, _ := appstate.Load(runtime.paths.State)
			if profile.ID == state.ActiveProfileID {
				return errors.New("cannot remove the active account; switch or deactivate first")
			}
			removed, err := data.Remove(args[0])
			if err != nil {
				return err
			}
			if err := runtime.manager.Save(data); err != nil {
				return err
			}
			return options.render(map[string]string{"removed": removed.Alias}, fmt.Sprintf("Removed %q", removed.Alias))
		},
	}
}

func newUseCommand(options *Options) *cobra.Command {
	var allowRunning bool
	var launch bool
	command := &cobra.Command{
		Use:   "use <alias>",
		Short: "Atomically switch the active Codex account",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			runtime, err := options.loadRuntime(false)
			if err != nil {
				return err
			}
			result, err := runtime.switcher().Use(args[0], allowRunning)
			if err != nil {
				return err
			}
			if launch {
				if err := launcher.Codex(); err != nil {
					return fmt.Errorf("account switched but Codex could not be launched: %w", err)
				}
			}
			message := fmt.Sprintf("Active account: %s. Restart Codex to apply it.", result.Alias)
			if !result.Changed {
				message = fmt.Sprintf("Account %s is already active.", result.Alias)
			}
			return options.render(result, message)
		},
	}
	command.Flags().BoolVar(&allowRunning, "allow-running", false, "switch even when a Codex process is detected")
	command.Flags().BoolVar(&launch, "launch", false, "launch Codex after switching")
	return command
}

func newDeactivateCommand(options *Options) *cobra.Command {
	var allowRunning bool
	command := &cobra.Command{
		Use:   "deactivate",
		Short: "Remove the active auth projection without deleting saved accounts",
		RunE: func(*cobra.Command, []string) error {
			runtime, err := options.loadRuntime(false)
			if err != nil {
				return err
			}
			if err := runtime.switcher().Deactivate(allowRunning); err != nil {
				return err
			}
			return options.render(map[string]bool{"deactivated": true}, "Removed the active Codex authentication projection.")
		},
	}
	command.Flags().BoolVar(&allowRunning, "allow-running", false, "deactivate even when a Codex process is detected")
	return command
}

func newCurrentCommand(options *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "current",
		Short: "Show the active account",
		RunE: func(*cobra.Command, []string) error {
			view, err := options.currentView()
			if err != nil {
				return err
			}
			return options.render(view, formatView(view))
		},
	}
}

func newStatusCommand(options *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Recover interrupted state and show the active account",
		RunE: func(*cobra.Command, []string) error {
			runtime, err := options.loadRuntime(false)
			if err != nil {
				return err
			}
			if err := runtime.switcher().Recover(); err != nil {
				return err
			}
			view, err := options.currentView()
			if err != nil {
				return err
			}
			result := map[string]any{"codexHome": runtime.home.Path, "active": view}
			return options.render(result, fmt.Sprintf("CODEX_HOME: %s\n%s", runtime.home.Path, formatView(view)))
		},
	}
}

func newDoctorCommand(options *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Run redacted local diagnostics",
		RunE: func(*cobra.Command, []string) error {
			runtime, err := options.loadRuntimeForDoctor()
			if err != nil {
				return err
			}
			report := doctor.Run(runtime.home, runtime.paths, runtime.manager, runtime.bin, runtime.binErr)
			if options.JSON {
				return options.render(report, "")
			}
			for _, check := range report.Checks {
				_, _ = fmt.Fprintf(options.Output, "%-8s %-20s %s\n", strings.ToUpper(check.Status), check.Name, check.Message)
			}
			if !report.OK {
				return errors.New("doctor found blocking problems")
			}
			return nil
		},
	}
}

func newSelectCommand(options *Options) *cobra.Command {
	var allowRunning bool
	command := &cobra.Command{
		Use:   "select",
		Short: "Interactively select an account",
		RunE: func(*cobra.Command, []string) error {
			runtime, err := options.loadRuntime(false)
			if err != nil {
				return err
			}
			data, err := runtime.manager.Load()
			if err != nil {
				return err
			}
			if len(data.Profiles) == 0 {
				return errors.New("no accounts saved")
			}
			for index, profile := range data.Profiles {
				_, _ = fmt.Fprintf(options.Output, "%d) %s\n", index+1, profile.Alias)
			}
			_, _ = fmt.Fprint(options.Output, "Select account: ")
			line, err := bufio.NewReader(options.Input).ReadString('\n')
			if err != nil && !errors.Is(err, io.EOF) {
				return err
			}
			choice, err := strconv.Atoi(strings.TrimSpace(line))
			if err != nil || choice < 1 || choice > len(data.Profiles) {
				return errors.New("invalid account selection")
			}
			result, err := runtime.switcher().Use(data.Profiles[choice-1].Alias, allowRunning)
			if err != nil {
				return err
			}
			return options.render(result, fmt.Sprintf("Active account: %s. Restart Codex to apply it.", result.Alias))
		},
	}
	command.Flags().BoolVar(&allowRunning, "allow-running", false, "switch even when a Codex process is detected")
	return command
}

func newVaultCommand(options *Options) *cobra.Command {
	command := &cobra.Command{Use: "vault", Short: "Back up and maintain the encrypted account vault"}
	var exportOutput string
	exportCommand := &cobra.Command{
		Use:   "export",
		Short: "Create a passphrase-encrypted portable backup",
		RunE: func(*cobra.Command, []string) error {
			if exportOutput == "" {
				return errors.New("--output is required")
			}
			runtime, err := options.loadRuntime(false)
			if err != nil {
				return err
			}
			data, err := runtime.manager.Load()
			if err != nil {
				return err
			}
			passphrase, err := options.readPassphrase("Export passphrase: ", true)
			if err != nil {
				return err
			}
			encoded, err := vault.Export(data, passphrase)
			if err != nil {
				return err
			}
			if err := atomicfile.Write(exportOutput, append(encoded, '\n'), 0o600); err != nil {
				return err
			}
			return options.render(map[string]string{"output": exportOutput}, "Exported encrypted vault backup to "+exportOutput)
		},
	}
	exportCommand.Flags().StringVarP(&exportOutput, "output", "o", "", "backup output path")

	var importInput string
	var replace bool
	importCommand := &cobra.Command{
		Use:   "import",
		Short: "Import a passphrase-encrypted portable backup",
		RunE: func(*cobra.Command, []string) error {
			if importInput == "" {
				return errors.New("--input is required")
			}
			runtime, err := options.loadRuntime(true)
			if err != nil {
				return err
			}
			encoded, err := atomicfile.ReadLimited(importInput, 64<<20)
			if err != nil {
				return err
			}
			passphrase, err := options.readPassphrase("Import passphrase: ", false)
			if err != nil {
				return err
			}
			imported, err := vault.Import(encoded, passphrase)
			if err != nil {
				return err
			}
			if !replace {
				current, err := runtime.manager.Load()
				if err != nil {
					return err
				}
				for _, profile := range imported.Profiles {
					if err := current.Add(profile, false); err != nil {
						return fmt.Errorf("merge imported profile %q: %w", profile.Alias, err)
					}
				}
				imported = current
			}
			if err := runtime.manager.Save(imported); err != nil {
				return err
			}
			return options.render(map[string]int{"profiles": len(imported.Profiles)}, fmt.Sprintf("Imported %d account profile(s)", len(imported.Profiles)))
		},
	}
	importCommand.Flags().StringVarP(&importInput, "input", "i", "", "backup input path")
	importCommand.Flags().BoolVar(&replace, "replace", false, "replace the local vault instead of merging")

	rotateCommand := &cobra.Command{
		Use:   "rotate-key",
		Short: "Rotate the operating-system-protected vault key",
		RunE: func(*cobra.Command, []string) error {
			runtime, err := options.loadRuntime(false)
			if err != nil {
				return err
			}
			if err := runtime.manager.RotateKey(); err != nil {
				return err
			}
			return options.render(map[string]bool{"rotated": true}, "Rotated the vault key.")
		},
	}
	command.AddCommand(exportCommand, importCommand, rotateCommand)
	return command
}

func newVersionCommand(options *Options) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the codex-switch version",
		RunE: func(*cobra.Command, []string) error {
			return options.render(map[string]string{"version": options.Version}, options.Version)
		},
	}
}

func newUpdateCommand(options *Options) *cobra.Command {
	var check bool
	command := &cobra.Command{
		Use:   "update",
		Short: "Securely update codex-switch from its latest GitHub release",
		RunE: func(*cobra.Command, []string) error {
			result, err := cliupdate.Run(context.Background(), options.Version, check)
			if err != nil {
				return err
			}
			message := fmt.Sprintf("codex-switch %s is available.", result.Latest)
			if result.Current {
				message = fmt.Sprintf("codex-switch %s is already current.", result.Latest)
			} else if result.Updated {
				message = fmt.Sprintf("Updated codex-switch to %s.", result.Latest)
			}
			return options.render(result, message)
		},
	}
	command.Flags().BoolVar(&check, "check", false, "check for an update without installing it")
	return command
}

func newCompletionCommand(root *cobra.Command) *cobra.Command {
	return &cobra.Command{
		Use:       "completion [bash|zsh|fish|powershell]",
		Short:     "Generate shell completion",
		Args:      cobra.ExactArgs(1),
		ValidArgs: []string{"bash", "zsh", "fish", "powershell"},
		RunE: func(command *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return root.GenBashCompletion(command.OutOrStdout())
			case "zsh":
				return root.GenZshCompletion(command.OutOrStdout())
			case "fish":
				return root.GenFishCompletion(command.OutOrStdout(), true)
			case "powershell":
				return root.GenPowerShellCompletion(command.OutOrStdout())
			default:
				return errors.New("unsupported shell")
			}
		},
	}
}

func (options *Options) loadRuntime(create bool) (runtimeState, error) {
	home, err := codexhome.Resolve(options.CodexHome)
	if err != nil {
		return runtimeState{}, err
	}
	paths, err := appconfig.ResolvePaths(options.AppHome)
	if err != nil {
		return runtimeState{}, err
	}
	if create {
		if err := paths.Ensure(); err != nil {
			return runtimeState{}, err
		}
		if err := home.Ensure(); err != nil {
			return runtimeState{}, err
		}
	}
	store, err := secretstore.Open()
	if err != nil {
		return runtimeState{}, err
	}
	manager := vault.New(paths.Vault, store)
	if create {
		if _, err := manager.Init(); err != nil {
			return runtimeState{}, err
		}
	}
	bin, binErr := codexlogin.FindBinary(options.CodexBin)
	return runtimeState{home: home, paths: paths, manager: manager, bin: bin, binErr: binErr}, nil
}

func (options *Options) loadRuntimeForDoctor() (runtimeState, error) {
	home, err := codexhome.Resolve(options.CodexHome)
	if err != nil {
		return runtimeState{}, err
	}
	paths, err := appconfig.ResolvePaths(options.AppHome)
	if err != nil {
		return runtimeState{}, err
	}
	store, storeErr := secretstore.Open()
	if storeErr != nil {
		return runtimeState{}, storeErr
	}
	bin, binErr := codexlogin.FindBinary(options.CodexBin)
	return runtimeState{home: home, paths: paths, manager: vault.New(paths.Vault, store), bin: bin, binErr: binErr}, nil
}

func (runtime runtimeState) switcher() switcher.Service {
	return switcher.Service{Home: runtime.home, Paths: runtime.paths, Vault: runtime.manager}
}

func (runtime runtimeState) usageService(version string) accountusage.Service {
	return accountusage.Service{
		Home:  runtime.home,
		Paths: runtime.paths,
		Vault: runtime.manager,
		Runner: codexusage.Runner{
			Binary:        runtime.bin,
			BinaryError:   runtime.binErr,
			ClientVersion: version,
		},
	}
}

func (options *Options) currentView() (accountView, error) {
	runtime, err := options.loadRuntime(false)
	if err != nil {
		return accountView{}, err
	}
	data, err := runtime.manager.Load()
	if err != nil {
		return accountView{}, err
	}
	state, err := appstate.Load(runtime.paths.State)
	if err != nil {
		return accountView{}, err
	}
	raw, err := runtime.home.ReadAuth()
	if errors.Is(err, os.ErrNotExist) {
		return accountView{}, errors.New("no account is active")
	}
	if err != nil {
		return accountView{}, err
	}
	document, err := authschema.Parse(raw)
	if err != nil {
		return accountView{}, err
	}
	if state.ActiveProfileID != "" {
		if profile, findErr := data.Find(state.ActiveProfileID); findErr == nil && profile.AccountID == document.Tokens.AccountID {
			return toView(*profile, true), nil
		}
	}
	matches := data.FindByAccount(document.Tokens.AccountID, document.WorkspaceID)
	if len(matches) == 1 {
		return toView(*matches[0], true), nil
	}
	return accountView{AccountID: document.Tokens.AccountID, WorkspaceID: document.WorkspaceID, Email: document.Email, Active: true, Alias: "unmanaged"}, nil
}

func addDocument(manager *vault.Manager, alias, source string, document authschema.Document, replace bool) (vault.Profile, error) {
	data, err := manager.Load()
	if err != nil {
		return vault.Profile{}, err
	}
	updatedAt, _ := document.GenerationTime()
	profile := vault.NewProfile(alias, source, document.Raw, document.Tokens.AccountID, document.WorkspaceID, document.Email, updatedAt)
	if err := data.Add(profile, replace); err != nil {
		return vault.Profile{}, err
	}
	if err := manager.Save(data); err != nil {
		return vault.Profile{}, err
	}
	saved, err := data.Find(alias)
	if err != nil {
		return vault.Profile{}, err
	}
	return *saved, nil
}

func toView(profile vault.Profile, active bool) accountView {
	return accountView{ID: profile.ID, Alias: profile.Alias, AccountID: profile.AccountID, WorkspaceID: profile.WorkspaceID, Email: profile.Email, Source: profile.Source, Active: active, AuthenticatedAt: profile.AuthenticatedAt, LastUsedAt: profile.LastUsedAt}
}

func formatView(view accountView) string {
	identity := view.Email
	if identity == "" {
		identity = view.AccountID
	}
	if view.Alias == "" {
		return identity
	}
	return fmt.Sprintf("%s (%s)", view.Alias, identity)
}

func usageFromCache(cache map[string]codexusage.Snapshot, profileID, queryError string, now time.Time) *usageView {
	snapshot, ok := cache[profileID]
	if !ok {
		return &usageView{Status: "unavailable", Error: queryError}
	}
	return &usageView{
		Status:     usageStatus(snapshot.FetchedAt, now),
		FetchedAt:  snapshot.FetchedAt,
		PlanType:   snapshot.PlanType,
		RateLimits: snapshot.RateLimits,
		TokenUsage: snapshot.TokenUsage,
		Partial:    snapshot.Partial,
		Error:      queryError,
	}
}

func usageStatus(fetchedAt, now time.Time) string {
	if fetchedAt.IsZero() || now.Sub(fetchedAt) > time.Minute {
		return "stale"
	}
	return "fresh"
}

func summarizeUsage(usage *usageView, now time.Time) (string, string, string, string) {
	if usage == nil || usage.Status == "unavailable" {
		return "-", "unavailable", "-", "-"
	}
	plan := usage.PlanType
	if plan == "" {
		plan = "-"
	}
	limits := "-"
	if main := mainRateLimit(usage.RateLimits); main != nil {
		parts := make([]string, 0, 2)
		if main.Primary != nil {
			parts = append(parts, compactWindow(main.Primary))
		}
		if main.Secondary != nil {
			parts = append(parts, compactWindow(main.Secondary))
		}
		if len(parts) > 0 {
			limits = strings.Join(parts, " · ")
		}
	}
	tokens := "-"
	if usage.TokenUsage != nil && usage.TokenUsage.Summary.LifetimeTokens != nil {
		tokens = compactNumber(*usage.TokenUsage.Summary.LifetimeTokens)
	}
	updated := relativeTime(usage.FetchedAt, now)
	if usage.Status == "stale" {
		updated += " (stale)"
	}
	if usage.Error != "" {
		updated += " (error)"
	}
	return plan, limits, tokens, updated
}

func formatUsage(view accountView, now time.Time) string {
	var output strings.Builder
	output.WriteString(formatView(view))
	if view.Active {
		output.WriteString(" [active]")
	}
	usage := view.Usage
	if usage == nil || usage.Status == "unavailable" {
		output.WriteString("\nUsage: unavailable")
		if usage != nil && usage.Error != "" {
			output.WriteString("\nError: ")
			output.WriteString(usage.Error)
		}
		return output.String()
	}
	plan := usage.PlanType
	if plan == "" {
		plan = "unknown"
	}
	fmt.Fprintf(&output, "\nPlan: %s", plan)
	fmt.Fprintf(&output, "\nUpdated: %s (%s)", usage.FetchedAt.Local().Format(time.RFC3339), usage.Status)
	if usage.RateLimits != nil {
		output.WriteString("\nLimits:")
		limits := usage.RateLimits.RateLimitsByLimitID
		if len(limits) == 0 {
			limits = map[string]codexusage.RateLimitSnapshot{"codex": usage.RateLimits.RateLimits}
		}
		keys := make([]string, 0, len(limits))
		for key := range limits {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			limit := limits[key]
			label := key
			if limit.LimitName != nil && *limit.LimitName != "" {
				label = *limit.LimitName
			}
			windows := make([]string, 0, 2)
			if limit.Primary != nil {
				windows = append(windows, detailedWindow(limit.Primary, now))
			}
			if limit.Secondary != nil {
				windows = append(windows, detailedWindow(limit.Secondary, now))
			}
			value := "unavailable"
			if len(windows) > 0 {
				value = strings.Join(windows, "; ")
			}
			fmt.Fprintf(&output, "\n  %s: %s", label, value)
		}
	}
	if usage.TokenUsage != nil {
		summary := usage.TokenUsage.Summary
		parts := make([]string, 0, 5)
		if summary.LifetimeTokens != nil {
			parts = append(parts, compactNumber(*summary.LifetimeTokens)+" lifetime")
		}
		if summary.PeakDailyTokens != nil {
			parts = append(parts, compactNumber(*summary.PeakDailyTokens)+" peak/day")
		}
		if summary.CurrentStreakDays != nil {
			parts = append(parts, fmt.Sprintf("%dd current streak", *summary.CurrentStreakDays))
		}
		if summary.LongestStreakDays != nil {
			parts = append(parts, fmt.Sprintf("%dd longest streak", *summary.LongestStreakDays))
		}
		if summary.LongestRunningTurnSec != nil {
			parts = append(parts, (time.Duration(*summary.LongestRunningTurnSec)*time.Second).String()+" longest turn")
		}
		if len(parts) > 0 {
			output.WriteString("\nTokens: ")
			output.WriteString(strings.Join(parts, "; "))
		}
	}
	if len(usage.Partial) > 0 {
		output.WriteString("\nPartial: ")
		output.WriteString(strings.Join(usage.Partial, ", "))
	}
	if usage.Error != "" {
		output.WriteString("\nWarning: ")
		output.WriteString(usage.Error)
	}
	return output.String()
}

func mainRateLimit(limits *codexusage.RateLimits) *codexusage.RateLimitSnapshot {
	if limits == nil {
		return nil
	}
	if value, ok := limits.RateLimitsByLimitID["codex"]; ok {
		copy := value
		return &copy
	}
	copy := limits.RateLimits
	return &copy
}

func compactWindow(window *codexusage.RateLimitWindow) string {
	duration := "limit"
	if window.WindowDurationMins != nil {
		duration = compactMinutes(*window.WindowDurationMins)
	}
	return fmt.Sprintf("%s %d%%", duration, window.UsedPercent)
}

func detailedWindow(window *codexusage.RateLimitWindow, now time.Time) string {
	value := compactWindow(window) + " used"
	if window.ResetsAt != nil {
		reset := time.Unix(*window.ResetsAt, 0)
		if reset.After(now) {
			value += ", resets in " + compactDuration(reset.Sub(now))
		} else {
			value += ", reset pending"
		}
	}
	return value
}

func compactMinutes(minutes int64) string {
	if minutes%(60*24) == 0 {
		return fmt.Sprintf("%dd", minutes/(60*24))
	}
	if minutes%60 == 0 {
		return fmt.Sprintf("%dh", minutes/60)
	}
	return fmt.Sprintf("%dm", minutes)
}

func compactDuration(duration time.Duration) string {
	if duration < 0 {
		duration = 0
	}
	if duration >= 24*time.Hour {
		return fmt.Sprintf("%dd %dh", int(duration/(24*time.Hour)), int(duration%(24*time.Hour)/time.Hour))
	}
	if duration >= time.Hour {
		return fmt.Sprintf("%dh %dm", int(duration/time.Hour), int(duration%time.Hour/time.Minute))
	}
	return fmt.Sprintf("%dm", int(duration/time.Minute))
}

func compactNumber(value int64) string {
	switch {
	case value >= 1_000_000_000:
		return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.1f", float64(value)/1_000_000_000), "0"), ".") + "B"
	case value >= 1_000_000:
		return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.1f", float64(value)/1_000_000), "0"), ".") + "M"
	case value >= 1_000:
		return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.1f", float64(value)/1_000), "0"), ".") + "K"
	default:
		return strconv.FormatInt(value, 10)
	}
}

func relativeTime(value, now time.Time) string {
	if value.IsZero() {
		return "never"
	}
	age := now.Sub(value)
	if age < 0 {
		age = 0
	}
	if age < time.Minute {
		return "just now"
	}
	return compactDuration(age) + " ago"
}

func (options *Options) render(value any, text string) error {
	if options.JSON {
		encoder := json.NewEncoder(options.Output)
		encoder.SetIndent("", "  ")
		return encoder.Encode(value)
	}
	if text != "" {
		_, err := fmt.Fprintln(options.Output, text)
		return err
	}
	return nil
}

func (options *Options) readPassphrase(prompt string, confirm bool) ([]byte, error) {
	file, ok := options.Input.(*os.File)
	if ok && term.IsTerminal(int(file.Fd())) {
		_, _ = fmt.Fprint(options.Error, prompt)
		value, err := term.ReadPassword(int(file.Fd()))
		_, _ = fmt.Fprintln(options.Error)
		if err != nil {
			return nil, err
		}
		if confirm {
			_, _ = fmt.Fprint(options.Error, "Confirm passphrase: ")
			second, err := term.ReadPassword(int(file.Fd()))
			_, _ = fmt.Fprintln(options.Error)
			if err != nil {
				return nil, err
			}
			if string(value) != string(second) {
				return nil, errors.New("passphrases do not match")
			}
		}
		return value, nil
	}
	line, err := bufio.NewReader(options.Input).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return []byte(strings.TrimRight(line, "\r\n")), nil
}
