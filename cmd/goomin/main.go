package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/urfave/cli/v3"

	"github.com/ahobsonsayers/goomin/frontend"
	"github.com/ahobsonsayers/goomin/internal/garmin"
	"github.com/ahobsonsayers/goomin/internal/google"
	"github.com/ahobsonsayers/goomin/internal/server"
	"github.com/ahobsonsayers/goomin/internal/state"
	"github.com/ahobsonsayers/goomin/internal/syncer"
)

var command = &cli.Command{ //nolint:gochecknoglobals // urfave/cli wants one root command.
	Name:  "goomin",
	Usage: "sync Google Health exercises into Garmin Connect",
	Flags: []cli.Flag{
		&cli.IntFlag{Name: "port", Value: 8080, Usage: "port for the web page"},
		&cli.StringFlag{Name: "state", Value: "state.json", Usage: "file holding tokens and the ledger"},
		&cli.DurationFlag{Name: "interval", Value: 12 * time.Hour, Usage: "how often to sync"},
	},
	Action: serve,
	Commands: []*cli.Command{
		{
			Name:   "sync",
			Usage:  "run one sync and exit",
			Action: syncOnce,
		},
	},
}

type config struct {
	Port      int
	StatePath string
	PublicURL string
	Interval  time.Duration
}

type app struct {
	config config
	store  *state.Store
	google *google.Client
	sync   *syncer.Sync
}

func main() {
	_ = godotenv.Load()

	err := command.Run(context.Background(), os.Args)
	if err != nil {
		slog.Error("goomin stopped", "error", err)
		os.Exit(1)
	}
}

func serve(ctx context.Context, cmd *cli.Command) error {
	cfg := configFrom(cmd)

	application, err := newApp(cfg)
	if err != nil {
		return fmt.Errorf("build app: %w", err)
	}

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	scheduled := func() {
		runErr := application.sync.Run(ctx)
		if runErr != nil {
			slog.Error("scheduled sync failed", "error", runErr)
		}
	}

	go func() {
		scheduled()

		ticker := time.NewTicker(application.config.Interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				scheduled()
			}
		}
	}()

	trigger := func() {
		go func() {
			runErr := application.sync.Run(ctx)
			if runErr != nil {
				slog.Error("sync failed", "error", runErr)
			}
		}()
	}

	apiServer := server.NewServer(application.store, application.google, trigger)

	err = server.Start(ctx, application.config.Port, apiServer, frontend.DistFS())
	if err != nil {
		return fmt.Errorf("run server: %w", err)
	}

	return nil
}

func syncOnce(ctx context.Context, cmd *cli.Command) error {
	cfg := configFrom(cmd)

	application, err := newApp(cfg)
	if err != nil {
		return fmt.Errorf("build app: %w", err)
	}

	return application.sync.Run(ctx)
}

func configFrom(cmd *cli.Command) config {
	cfg := config{
		Port:      cmd.Int("port"),
		StatePath: cmd.String("state"),
		PublicURL: os.Getenv("PUBLIC_URL"),
		Interval:  cmd.Duration("interval"),
	}

	if cfg.PublicURL == "" {
		cfg.PublicURL = fmt.Sprintf("http://127.0.0.1:%d", cfg.Port)
	}

	return cfg
}

func newApp(cfg config) (*app, error) {
	store, err := state.Open(cfg.StatePath)
	if err != nil {
		return nil, fmt.Errorf("open state: %w", err)
	}

	client := &google.Client{
		ClientID:     os.Getenv("GOOGLE_CLIENT_ID"),
		ClientSecret: os.Getenv("GOOGLE_CLIENT_SECRET"),
		RedirectURL:  cfg.PublicURL + "/api/auth/google/callback",
	}

	syncRun := &syncer.Sync{
		Store:    store,
		Health:   client,
		Uploader: garminUploader{},
		Google:   googleTokens(store, client),
		Garmin:   garminTokens(store),
	}

	return &app{config: cfg, store: store, google: client, sync: syncRun}, nil
}

type garminUploader struct{}

func (garminUploader) Upload(ctx context.Context, accessToken string, tcx []byte, exerciseID string) (string, error) {
	return garmin.Upload(ctx, accessToken, tcx, exerciseID)
}
