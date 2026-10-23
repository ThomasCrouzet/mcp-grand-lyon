package service

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/assets"
	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/config"
	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/httpclient"
	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/model"
	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/providers"
	"github.com/ThomasCrouzet/mcp-grand-lyon/internal/storage"
)

type App struct {
	Config   config.Config
	Store    *storage.Store
	Provider model.Provider
	HTTP     *httpclient.Client
}

func New(ctx context.Context, cfg config.Config) (*App, error) {
	store, err := storage.Open(ctx, cfg.DBPath)
	if err != nil {
		return nil, err
	}
	complete := false
	defer func() {
		if !complete {
			_ = store.Close()
		}
	}()
	load := func(name, fallback string) ([]byte, error) {
		data, readErr := os.ReadFile(filepath.Join(cfg.ConfigDir, name))
		if errors.Is(readErr, os.ErrNotExist) {
			return assets.Files.ReadFile("config/" + fallback)
		}
		return data, readErr
	}
	sources, err := load("sources.yaml", "sources.example.yaml")
	if err != nil {
		return nil, err
	}
	if err = store.LoadSources(ctx, sources); err != nil {
		return nil, err
	}
	profiles, err := os.ReadFile(filepath.Join(cfg.ConfigDir, "profiles.yaml"))
	if errors.Is(err, os.ErrNotExist) {
		var count int
		if err = store.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM user_places").Scan(&count); err != nil {
			return nil, err
		}
		if count == 0 {
			profiles, err = assets.Files.ReadFile("config/profiles.example.yaml")
		}
	}
	if err != nil {
		return nil, err
	}
	if len(profiles) > 0 {
		if err = store.LoadProfiles(ctx, profiles); err != nil {
			return nil, err
		}
	}
	if cfg.Username == "" || cfg.Password == "" {
		cfg.Offline = true
	}
	http := httpclient.New(cfg)
	app := &App{Config: cfg, Store: store, HTTP: http}
	if cfg.Offline {
		var fixtures fs.FS
		if cfg.FixturesDir != "" {
			fixtures = os.DirFS(cfg.FixturesDir)
		} else {
			fixtures, err = fs.Sub(assets.Files, "fixtures")
			if err != nil {
				_ = http.Close()
				return nil, err
			}
		}
		app.Provider = providers.NewFixtures(fixtures)
	} else {
		app.Provider = providers.NewLive(http, store, cfg)
	}
	complete = true
	return app, nil
}

func (app *App) Close() error {
	var errs []error
	if app.HTTP != nil {
		errs = append(errs, app.HTTP.Close())
	}
	if app.Store != nil {
		errs = append(errs, app.Store.Close())
	}
	return errors.Join(errs...)
}
