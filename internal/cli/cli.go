// Package cli implements the grand-lyon-mcp command-line interface.
package cli

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/adapter"
	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/config"
	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/model"
	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/service"
	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/storage"
	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/version"
)

const (
	gtfsURL      = "https://download.data.grandlyon.com/files/rdata/tcl_sytral.tcltheorique/GTFS_TCL.ZIP"
	gtfsMaxBytes = 300 * 1024 * 1024
)

var publicTools = []string{
	"lyon_resolve_place",
	"lyon_next_departures",
	"lyon_mobility_status",
	"lyon_trip_options",
	"lyon_parking_options",
	"lyon_accessibility_check",
	"lyon_nearby_facilities",
	"lyon_environment_brief",
	"lyon_waste_dropoff",
	"lyon_personal_briefing",
}

type commandError struct {
	code int
	err  error
}

func (failure commandError) Error() string { return failure.err.Error() }

// Run executes one CLI invocation and returns its exit code.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	err := execute(ctx, args, stdout, stderr)
	if err == nil {
		return 0
	}
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if ctx.Err() != nil && errors.Is(err, context.Canceled) {
		return 0
	}
	code := 1
	var failure commandError
	if errors.As(err, &failure) {
		code = failure.code
	}
	fmt.Fprintf(stderr, "Error: %v\n", err)
	return code
}

func execute(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 || args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		writeHelp(stdout)
		return nil
	}
	switch args[0] {
	case "version":
		if err := requireNoArguments(args[1:]); err != nil {
			return err
		}
		fmt.Fprintln(stdout, version.Version)
		return nil
	case "setup":
		return runSetup(ctx, args[1:], stdout, stderr)
	case "config":
		return runConfig(args[1:], stdout, stderr)
	case "history":
		return runHistory(ctx, args[1:], stdout, stderr)
	case "env":
		return runEnvironment(args[1:], stdout, stderr)
	case "client-config":
		return runClientConfig(args[1:], stdout, stderr)
	case "doctor":
		return runDoctor(ctx, args[1:], stdout, stderr)
	case "db":
		return runDatabase(ctx, args[1:], stdout, stderr)
	case "sync":
		return runSync(ctx, args[1:], stdout, stderr)
	case "catalog":
		return runCatalog(ctx, args[1:], stdout, stderr)
	case "snapshot":
		return runSnapshot(ctx, args[1:], stdout, stderr)
	case "smoke":
		return runSmoke(ctx, args[1:], stdout, stderr)
	case "serve":
		return runServe(ctx, args[1:], stderr)
	default:
		return usageError("unknown command %q", args[0])
	}
}

func runSetup(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := newFlagSet("setup", stderr)
	yes := flags.Bool("yes", false, "use noninteractive defaults")
	flags.BoolVar(yes, "y", false, "use noninteractive defaults")
	offline := flags.Bool("offline", false, "use fixture providers")
	live := flags.Bool("live", false, "use live providers")
	force := flags.Bool("force-config", false, "replace existing configuration files")
	skipInstall := flags.Bool("skip-install", false, "accepted compatibility option")
	skipMigrate := flags.Bool("skip-migrate", false, "skip database migration")
	if err := parseFlags(flags, args); err != nil {
		return err
	}
	if *offline && *live {
		return usageError("--offline and --live are mutually exclusive")
	}
	cfg, err := config.LoadForSetup(*force)
	if err != nil {
		return err
	}
	_ = *skipInstall
	if *offline {
		cfg.Offline = true
	} else if *live {
		cfg.Offline = false
	} else if *yes {
		cfg.Offline = true
	} else {
		selected, promptErr := promptOffline(stdout)
		if promptErr != nil {
			return promptErr
		}
		cfg.Offline = selected
	}
	if !cfg.Offline && (cfg.Username == "" || cfg.Password == "") {
		return usageError("live setup requires DATAGRANDLYON_USERNAME and DATAGRANDLYON_PASSWORD")
	}
	if err := copyConfiguration(cfg, *force); err != nil {
		return err
	}
	migration := config.MigrationResult{Migrated: []string{}, Conflicts: []config.MigrationConflict{}, Removed: []string{}}
	if !*force {
		migration, err = config.Migrate(cfg)
		if err != nil {
			return err
		}
	}
	if err := config.WriteSecrets(cfg); err != nil {
		return err
	}
	if !*skipMigrate {
		app, appErr := service.New(ctx, cfg)
		if appErr != nil {
			return appErr
		}
		if _, infoErr := app.Store.Info(ctx); infoErr != nil {
			_ = app.Close()
			return infoErr
		}
		if closeErr := app.Close(); closeErr != nil {
			return closeErr
		}
	}
	executable, err := executablePath()
	if err != nil {
		return err
	}
	snippet, err := writeClientConfig(cfg, executable)
	if err != nil {
		return err
	}
	return writeJSON(stdout, map[string]any{
		"status":        "OK",
		"mode":          map[bool]string{true: "offline", false: "live"}[cfg.Offline],
		"config_dir":    cfg.ConfigDir,
		"data_dir":      cfg.DataDir,
		"db_path":       cfg.DBPath,
		"client_config": snippet,
		"migration":     migration,
	})
}

func runConfig(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return usageError("config requires explain or migrate")
	}
	switch args[0] {
	case "explain":
		if err := requireNoArguments(args[1:]); err != nil {
			return err
		}
		report, err := config.Explain()
		if err != nil {
			return err
		}
		return writeJSON(stdout, report)
	case "migrate":
		if err := requireNoArguments(args[1:]); err != nil {
			return err
		}
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		result, err := config.Migrate(cfg)
		if err != nil {
			return err
		}
		return writeJSON(stdout, result)
	default:
		return usageError("unknown config command %q", args[0])
	}
}

func runEnvironment(args []string, stdout, stderr io.Writer) error {
	flags := newFlagSet("env", stderr)
	edit := flags.Bool("edit", false, "open secrets.env in EDITOR")
	configDirOnly := flags.Bool("config-dir", false, "print only the resolved configuration directory")
	if err := parseFlags(flags, args); err != nil {
		return err
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if *configDirOnly {
		fmt.Fprintln(stdout, cfg.ConfigDir)
		return nil
	}
	secretsPath := filepath.Join(cfg.ConfigDir, "secrets.env")
	if *edit {
		if _, err := os.Stat(secretsPath); err != nil {
			return fmt.Errorf("open secrets file: %w", err)
		}
		editor := os.Getenv("EDITOR")
		if editor == "" {
			editor = "nano"
		}
		command := exec.Command(editor, secretsPath)
		command.Stdin = os.Stdin
		command.Stdout = stdout
		command.Stderr = stderr
		return command.Run()
	}
	_, statErr := os.Stat(secretsPath)
	secretsExist := statErr == nil
	if statErr != nil && !os.IsNotExist(statErr) {
		return statErr
	}
	values := configStatus(cfg)
	return writeJSON(stdout, map[string]any{
		"config_dir":     cfg.ConfigDir,
		"secrets_file":   secretsPath,
		"secrets_exists": secretsExist,
		"values":         values,
	})
}

func runClientConfig(args []string, stdout, stderr io.Writer) error {
	flags := newFlagSet("client-config", stderr)
	wrapper := flags.Bool("wrapper", false, "accepted compatibility option")
	binary := flags.Bool("bin", false, "use the installed binary")
	if err := parseFlags(flags, args); err != nil {
		return err
	}
	_, _ = *wrapper, *binary
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	executable, err := executablePath()
	if err != nil {
		return err
	}
	return writeJSON(stdout, clientConfig(cfg, executable))
}

func runDoctor(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if err := requireNoArguments(args); err != nil {
		return err
	}
	return withApp(ctx, func(cfg config.Config, app *service.App) error {
		info, err := app.Store.Info(ctx)
		if err != nil {
			return err
		}
		credentialStatus := "missing"
		if cfg.Username != "" && cfg.Password != "" {
			credentialStatus = "[set]"
		}
		sources, err := app.Store.ListSources(ctx)
		if err != nil {
			return err
		}
		sourceStatus := make([]map[string]any, 0, len(sources))
		for _, source := range sources {
			sourceStatus = append(sourceStatus, map[string]any{
				"source_id": source["source_id"], "enabled": source["enabled"],
				"status": source["status"], "service": source["service"],
				"table_name": source["table_name"],
			})
		}
		capabilities, err := app.SourceCapabilities(ctx)
		if err != nil {
			return err
		}
		history, err := app.Store.HistoryInfo(ctx, service.HistoryRetentionPolicy(cfg))
		if err != nil {
			return err
		}
		if stat, statErr := os.Stat(cfg.DBPath); statErr == nil {
			history["db_size_bytes"] = stat.Size()
		}
		gtfs, err := app.Store.GTFSCoverage(ctx, model.Now())
		if err != nil {
			return err
		}
		modeReason := "explicit offline mode"
		if !cfg.Offline {
			modeReason = "live mode requested"
			if !app.LiveCredentialsPresent() {
				modeReason = "live mode requested; DataGrandLyon credentials are missing"
			}
		}
		if err := writeJSON(stdout, map[string]any{
			"status":                   "OK",
			"requested_mode":           app.RequestedMode(),
			"effective_mode":           app.EffectiveMode(),
			"mode_reason":              modeReason,
			"offline":                  cfg.Offline,
			"credentials":              credentialStatus,
			"local_installation":       "OK",
			"config_dir":               cfg.ConfigDir,
			"data_dir":                 cfg.DataDir,
			"db_path":                  cfg.DBPath,
			"database":                 info,
			"storage":                  storage.InspectStorage(cfg.DataDir, cfg.DBPath),
			"history":                  history,
			"gtfs":                     gtfs,
			"sources":                  sourceStatus,
			"source_capabilities":      capabilities,
			"transitous_enabled":       cfg.TransitousEnabled,
			"tls_verification_enabled": cfg.VerifyTLS,
		}); err != nil {
			return err
		}
		if !cfg.Offline && !app.LiveCredentialsPresent() {
			return commandError{code: 3, err: errors.New("live mode is missing DataGrandLyon credentials; credential-dependent sources are unavailable")}
		}
		return nil
	})
}

func runHistory(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return usageError("history requires info, prune, or delete")
	}
	switch args[0] {
	case "info", "prune":
		if err := requireNoArguments(args[1:]); err != nil {
			return err
		}
		return withApp(ctx, func(cfg config.Config, app *service.App) error {
			policy := service.HistoryRetentionPolicy(cfg)
			if args[0] == "prune" {
				removed, err := app.Store.PruneHistory(ctx, policy)
				if err != nil {
					return err
				}
				return writeJSON(stdout, map[string]any{"status": "OK", "removed": removed, "retention": policy})
			}
			info, err := app.Store.HistoryInfo(ctx, policy)
			if err != nil {
				return err
			}
			info["db_path"] = cfg.DBPath
			if stat, statErr := os.Stat(cfg.DBPath); statErr == nil {
				info["db_size_bytes"] = stat.Size()
			}
			return writeJSON(stdout, info)
		})
	case "delete":
		flags := newFlagSet("history delete", stderr)
		profile := flags.String("profile", "", "delete briefing history for one profile")
		station := flags.String("station", "", "delete bicycle history for one station")
		allVelov := flags.Bool("all-velov", false, "delete all bicycle history")
		if err := parseFlags(flags, args[1:]); err != nil {
			return err
		}
		if *profile == "" && *station == "" && !*allVelov {
			return usageError("history delete requires --profile, --station, or --all-velov")
		}
		return withApp(ctx, func(_ config.Config, app *service.App) error {
			result := map[string]any{"status": "OK", "briefing_deleted": 0, "velov_deleted": 0}
			if *profile != "" {
				deleted, err := app.Store.DeleteBriefingHistory(ctx, *profile)
				if err != nil {
					return err
				}
				result["briefing_deleted"] = deleted
				result["profile"] = *profile
			}
			if *station != "" || *allVelov {
				deleted, err := app.Store.DeleteVelovHistory(ctx, *station)
				if err != nil {
					return err
				}
				result["velov_deleted"] = deleted
				if *station != "" {
					result["station"] = *station
				}
			}
			return writeJSON(stdout, result)
		})
	default:
		return usageError("unknown history command %q", args[0])
	}
}

func runDatabase(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return usageError("db requires migrate, info, or secure")
	}
	command := args[0]
	if command != "migrate" && command != "info" && command != "secure" {
		return usageError("unknown db command %q", command)
	}
	if err := requireNoArguments(args[1:]); err != nil {
		return err
	}
	return withApp(ctx, func(cfg config.Config, app *service.App) error {
		if command == "secure" {
			result, err := storage.SecureStorage(cfg.DataDir, cfg.DBPath)
			if err != nil {
				return err
			}
			return writeJSON(stdout, result)
		}
		info, err := app.Store.Info(ctx)
		if err != nil {
			return err
		}
		if command == "migrate" {
			return writeJSON(stdout, map[string]any{
				"applied": info["migrations_applied"],
				"db":      cfg.DBPath,
			})
		}
		info["path"] = cfg.DBPath
		return writeJSON(stdout, info)
	})
}

func runSync(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return usageError("sync requires gtfs or static")
	}
	switch args[0] {
	case "static":
		if err := requireNoArguments(args[1:]); err != nil {
			return err
		}
		return writeJSON(stdout, map[string]any{
			"status":  "UNSUPPORTED",
			"message": "static facility indexing is not implemented",
		})
	case "gtfs":
		flags := newFlagSet("sync gtfs", stderr)
		fromFile := flags.String("from-file", "", "import a local GTFS archive")
		maintain := flags.Bool("maintain", false, "run bounded maintenance and skip an unchanged archive")
		date := flags.String("date", "", "service date to check (YYYY-MM-DD)")
		if err := parseFlags(flags, args[1:]); err != nil {
			return err
		}
		return withApp(ctx, func(cfg config.Config, app *service.App) error {
			if *maintain {
				return maintainGTFS(ctx, cfg, app, *fromFile, *date, stdout)
			}
			path := *fromFile
			if path == "" && cfg.Offline {
				return writeJSON(stdout, map[string]any{
					"status": "DISABLED",
					"reason": "offline and no --from-file",
				})
			}
			if path == "" {
				path = filepath.Join(cfg.DataDir, "gtfs", "GTFS_TCL.ZIP")
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					return fmt.Errorf("create GTFS data directory: %w", err)
				}
				response, err := app.HTTP.Download(ctx, gtfsURL, path, true, gtfsMaxBytes)
				if err != nil {
					return err
				}
				if response.StatusCode != http.StatusOK {
					return fmt.Errorf("GTFS download returned HTTP %d", response.StatusCode)
				}
			}
			counts, err := app.Store.ImportGTFS(ctx, path)
			if err != nil {
				return err
			}
			return writeJSON(stdout, map[string]any{
				"status": "OK", "counts": counts, "entities_indexed": counts["entities_indexed"],
			})
		})
	default:
		return usageError("unknown sync command %q", args[0])
	}
}

func maintainGTFS(ctx context.Context, cfg config.Config, app *service.App, fromFile, date string, stdout io.Writer) error {
	checkDate := model.Now()
	if date != "" {
		parsed, err := time.ParseInLocation("2006-01-02", date, model.Paris)
		if err != nil {
			return usageError("--date must use YYYY-MM-DD")
		}
		checkDate = parsed
	}
	coverageBefore, err := app.Store.GTFSCoverage(ctx, checkDate)
	if err != nil {
		return err
	}
	active, err := app.Store.ActiveGTFSImport(ctx)
	if err != nil {
		return err
	}
	path := fromFile
	downloaded := false
	if path == "" {
		if cfg.Offline {
			return writeJSON(stdout, map[string]any{
				"status": "DISABLED", "reason": "offline and no --from-file",
				"coverage": coverageBefore,
			})
		}
		directory := filepath.Join(cfg.DataDir, "gtfs")
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return fmt.Errorf("create GTFS data directory: %w", err)
		}
		temporary, err := os.CreateTemp(directory, ".gtfs-maintenance-*")
		if err != nil {
			return fmt.Errorf("create GTFS maintenance file: %w", err)
		}
		path = temporary.Name()
		if err := temporary.Close(); err != nil {
			return err
		}
		defer os.Remove(path)
		response, err := app.HTTP.Download(ctx, gtfsURL, path, true, gtfsMaxBytes)
		if err != nil {
			return err
		}
		if response.StatusCode != http.StatusOK {
			return fmt.Errorf("GTFS download returned HTTP %d", response.StatusCode)
		}
		downloaded = true
	}
	hash, err := fileSHA256(path)
	if err != nil {
		return err
	}
	if active != nil && active["file_hash"] == hash {
		output := map[string]any{
			"status": "UNCHANGED", "unchanged": true, "archive_sha256": hash,
			"active_import": active, "coverage": coverageBefore,
		}
		if err := writeJSON(stdout, output); err != nil {
			return err
		}
		return maintenanceExit(coverageBefore)
	}
	if downloaded {
		canonical := filepath.Join(cfg.DataDir, "gtfs", "GTFS_TCL.ZIP")
		if err := os.Rename(path, canonical); err != nil {
			return fmt.Errorf("publish GTFS archive: %w", err)
		}
		path = canonical
	}
	counts, err := app.Store.ImportGTFS(ctx, path)
	if err != nil {
		return fmt.Errorf("import GTFS archive: %w", err)
	}
	coverageAfter, err := app.Store.GTFSCoverage(ctx, checkDate)
	if err != nil {
		return err
	}
	newActive, err := app.Store.ActiveGTFSImport(ctx)
	if err != nil {
		return err
	}
	output := map[string]any{
		"status": "OK", "unchanged": false, "counts": counts,
		"entities_indexed": counts["entities_indexed"], "archive_sha256": hash,
		"active_import": newActive, "coverage_before": coverageBefore, "coverage": coverageAfter,
	}
	if err := writeJSON(stdout, output); err != nil {
		return err
	}
	return maintenanceExit(coverageAfter)
}

func maintenanceExit(coverage map[string]any) error {
	switch coverage["status"] {
	case "current", "covered_no_service":
		return nil
	default:
		return commandError{code: 3, err: fmt.Errorf("GTFS coverage is %v; the timetable is imported but does not cover the checked date", coverage["status"])}
	}
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open GTFS archive: %w", err)
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", fmt.Errorf("hash GTFS archive: %w", err)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

type catalogProvider interface {
	CatalogScan(context.Context, string) (map[string]any, error)
	CatalogValidate(context.Context) (map[string]any, error)
}

func runCatalog(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return usageError("catalog requires scan or validate")
	}
	command := args[0]
	if command != "scan" && command != "validate" {
		return usageError("unknown catalog command %q", command)
	}
	flags := newFlagSet("catalog "+command, stderr)
	mode := flags.String("mode", "auto", "catalog scan mode")
	if err := parseFlags(flags, args[1:]); err != nil {
		return err
	}
	if command == "scan" && *mode != "auto" && *mode != "full" && *mode != "known" && *mode != "ogc" {
		return usageError("unknown catalog mode %q", *mode)
	}
	return withApp(ctx, func(cfg config.Config, app *service.App) error {
		if cfg.Offline {
			return writeJSON(stdout, map[string]any{"status": "DISABLED", "reason": "offline mode"})
		}
		provider, ok := app.Provider.(catalogProvider)
		if !ok {
			return errors.New("provider does not support catalog operations")
		}
		var result map[string]any
		var err error
		if command == "scan" {
			result, err = provider.CatalogScan(ctx, *mode)
		} else {
			result, err = provider.CatalogValidate(ctx)
		}
		if err != nil {
			return err
		}
		return writeJSON(stdout, result)
	})
}

func runSnapshot(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 || args[0] != "velov" {
		return usageError("snapshot requires velov")
	}
	if err := requireNoArguments(args[1:]); err != nil {
		return err
	}
	return withApp(ctx, func(_ config.Config, app *service.App) error {
		result := app.VelovStations(ctx, model.PlaceRef{
			Latitude: model.Ptr(45.76), Longitude: model.Ptr(4.85),
		}, 5000, 0, 50)
		return writeJSON(stdout, map[string]any{
			"status": result.Status,
			"count":  sliceLength(result.Data["stations"]),
		})
	})
}

func runSmoke(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := newFlagSet("smoke", stderr)
	selected := flags.String("tool", "", "run one public tool")
	if err := parseFlags(flags, args); err != nil {
		return err
	}
	names := publicTools
	if *selected != "" {
		if !contains(publicTools, *selected) {
			return usageError("unknown tool %q", *selected)
		}
		names = []string{*selected}
	}
	return withApp(ctx, func(cfg config.Config, app *service.App) error {
		fmt.Fprintf(stdout, "grand-lyon-mcp smoke, mode=%s db=%s\n", map[bool]string{true: "offline", false: "live"}[cfg.Offline], cfg.DBPath)
		passed := 0
		for _, name := range names {
			payload, err := json.Marshal(smokePayloads()[name])
			if err != nil {
				return err
			}
			result := app.Call(ctx, name, payload)
			accepted := contains([]string{"ok", "partial", "ambiguous", "not_found"}, result.Status)
			if accepted {
				passed++
			}
			fmt.Fprintf(stdout, "%s\t%s\t%s\n", name, result.Status, result.Summary)
		}
		fmt.Fprintf(stdout, "%d/%d tools returned an accepted envelope.\n", passed, len(names))
		if passed != len(names) {
			return errors.New("one or more smoke calls failed")
		}
		return nil
	})
}

func runServe(ctx context.Context, args []string, stderr io.Writer) error {
	flags := newFlagSet("serve", stderr)
	transport := flags.String("transport", "stdio", "MCP transport")
	quiet := flags.Bool("quiet", false, "suppress startup status")
	flags.BoolVar(quiet, "q", false, "suppress startup status")
	if err := parseFlags(flags, args); err != nil {
		return err
	}
	if *transport != "stdio" {
		return usageError("unsupported transport %q", *transport)
	}
	_ = *quiet
	return withApp(ctx, func(_ config.Config, app *service.App) error {
		return adapter.Run(ctx, app)
	})
}

func withApp(ctx context.Context, operation func(config.Config, *service.App) error) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	app, err := service.New(ctx, cfg)
	if err != nil {
		return err
	}
	operationErr := operation(app.Config, app)
	closeErr := app.Close()
	if operationErr != nil {
		return operationErr
	}
	return closeErr
}

func promptOffline(output io.Writer) (bool, error) {
	fmt.Fprint(output, "Use offline fixture mode? [Y/n]: ")
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "" || answer == "y" || answer == "yes" || answer == "o" || answer == "oui", nil
}

func configStatus(cfg config.Config) map[string]string {
	credential := func(value string) string {
		if value == "" {
			return "(empty)"
		}
		return "[set]"
	}
	return map[string]string{
		"DATAGRANDLYON_USERNAME":                      credential(cfg.Username),
		"DATAGRANDLYON_PASSWORD":                      credential(cfg.Password),
		"GRAND_LYON_MCP_CONFIG_DIR":                   cfg.ConfigDir,
		"GRAND_LYON_MCP_DATA_DIR":                     cfg.DataDir,
		"GRAND_LYON_MCP_DB_PATH":                      cfg.DBPath,
		"GRAND_LYON_MCP_FIXTURES_DIR":                 cfg.FixturesDir,
		"GRAND_LYON_MCP_LOG_LEVEL":                    cfg.LogLevel,
		"GRAND_LYON_MCP_OFFLINE":                      fmt.Sprint(cfg.Offline),
		"GRAND_LYON_MCP_VERIFY_TLS":                   fmt.Sprint(cfg.VerifyTLS),
		"GRAND_LYON_MCP_HTTP_CONNECT_TIMEOUT_SECONDS": formatDuration(cfg.ConnectTimeout),
		"GRAND_LYON_MCP_HTTP_READ_TIMEOUT_SECONDS":    formatDuration(cfg.ReadTimeout),
		"GRAND_LYON_MCP_MAX_PARALLEL_REQUESTS":        fmt.Sprint(cfg.MaxParallel),
		"GRAND_LYON_MCP_FUZZY_SCORE_THRESHOLD":        fmt.Sprint(cfg.FuzzyScoreThreshold),
		"TRANSITOUS_ENABLED":                          fmt.Sprint(cfg.TransitousEnabled),
		"TRANSITOUS_BASE_URL":                         cfg.TransitousBaseURL,
	}
}

func smokePayloads() map[string]map[string]any {
	return map[string]map[string]any{
		"lyon_resolve_place":       {"query": "Part-Dieu", "limit": 3},
		"lyon_next_departures":     {"stop": map[string]any{"query": "Bellecour"}, "line": "A", "limit": 3},
		"lyon_mobility_status":     {"lines": []string{"A"}, "include": []string{"transit", "traffic"}},
		"lyon_trip_options":        {"origin": map[string]any{"profile_place": "home"}, "destination": map[string]any{"profile_place": "work"}, "modes": []string{"walk", "velov", "tcl"}},
		"lyon_parking_options":     {"destination": map[string]any{"query": "Hôtel de Ville"}, "limit": 3},
		"lyon_accessibility_check": {"origin": map[string]any{"query": "Bellecour"}, "destination": map[string]any{"query": "Part-Dieu"}, "needs": []string{"wheelchair"}},
		"lyon_nearby_facilities":   {"location": map[string]any{"latitude": 45.777, "longitude": 4.855}, "categories": []string{"toilet", "park"}, "radius_m": 2000},
		"lyon_environment_brief":   {"location": map[string]any{"query": "Lyon"}, "indicators": []string{"pollen", "heat"}},
		"lyon_waste_dropoff":       {"item": "batterie de vélo électrique", "location": map[string]any{"profile_place": "home"}},
		"lyon_personal_briefing":   {"profile": "weekday_morning"},
	}
}

func writeHelp(output io.Writer) {
	fmt.Fprintln(output, "Usage: grand-lyon-mcp <command> [options]")
	fmt.Fprintln(output, "Commands: setup, config, env, client-config, doctor, db, history, sync, catalog, snapshot, smoke, serve, version")
}

func newFlagSet(name string, output io.Writer) *flag.FlagSet {
	flags := flag.NewFlagSet(name, flag.ContinueOnError)
	flags.SetOutput(output)
	return flags
}

func parseFlags(flags *flag.FlagSet, args []string) error {
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return err
		}
		return commandError{code: 2, err: err}
	}
	return requireNoArguments(flags.Args())
}

func requireNoArguments(args []string) error {
	if len(args) != 0 {
		return usageError("unexpected argument %q", args[0])
	}
	return nil
}

func usageError(format string, values ...any) error {
	return commandError{code: 2, err: fmt.Errorf(format, values...)}
}

func writeJSON(output io.Writer, value any) error {
	encoder := json.NewEncoder(output)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func formatDuration(value time.Duration) string {
	return fmt.Sprintf("%g", value.Seconds())
}

func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func sliceLength(value any) int {
	reflection := reflect.ValueOf(value)
	if reflection.IsValid() && (reflection.Kind() == reflect.Slice || reflection.Kind() == reflect.Array) {
		return reflection.Len()
	}
	return 0
}
