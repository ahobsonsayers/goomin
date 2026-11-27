package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/urfave/cli/v3"

	"github.com/ahobsonsayers/health-connectd/garmin"
	"github.com/ahobsonsayers/health-connectd/google"
	"github.com/ahobsonsayers/health-connectd/state"
	"github.com/ahobsonsayers/health-connectd/syncer"
	"github.com/ahobsonsayers/health-connectd/web"
	"github.com/ahobsonsayers/health-connectd/worker"
)

const shutdownTime = 10 * time.Second

var command = &cli.Command{ //nolint:gochecknoglobals // urfave/cli wants one root command.
	Name:  "health-connectd",
	Usage: "sync Google Health exercises into Garmin Connect",
	Flags: []cli.Flag{
		&cli.IntFlag{Name: "port", Value: 8080, Usage: "port for the web page"},
		&cli.StringFlag{Name: "bind", Value: "127.0.0.1", Usage: "address to listen on"},
		&cli.StringFlag{Name: "state", Value: "state.json", Usage: "file holding tokens and the ledger"},
		&cli.StringFlag{Name: "public-url", Usage: "URL the browser uses to reach this server"},
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
	Bind      string
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
		slog.Error("health-connectd stopped", "error", err)
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

	trigger := func() {
		go func() {
			runErr := application.sync.Run(ctx)
			if runErr != nil {
				slog.Error("sync failed", "error", runErr)
			}
		}()
	}

	handler, err := web.New(
		application.store,
		application.google,
		trigger,
		os.Getenv("UI_PASSWORD"),
	)
	if err != nil {
		return fmt.Errorf("build web server: %w", err)
	}

	server := &http.Server{ //nolint:exhaustruct // Only these fields matter here.
		Addr:              net.JoinHostPort(application.config.Bind, strconv.Itoa(application.config.Port)),
		Handler:           handler,
		ReadHeaderTimeout: shutdownTime,
	}

	go func() {
		slog.Info("listening", "url", application.config.PublicURL)

		listenErr := server.ListenAndServe()
		if listenErr != nil && !errors.Is(listenErr, http.ErrServerClosed) {
			slog.Error("web server stopped", "error", listenErr)
		}
	}()

	go worker.Every(ctx, application.config.Interval, func(ctx context.Context) {
		if !due(application.store, application.config.Interval) {
			return
		}

		runErr := application.sync.Run(ctx)
		if runErr != nil {
			slog.Error("scheduled sync failed", "error", runErr)
		}
	})

	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTime)
	defer cancel()

	return server.Shutdown(shutdownCtx)
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
		Bind:      cmd.String("bind"),
		StatePath: cmd.String("state"),
		PublicURL: cmd.String("public-url"),
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

func due(store *state.Store, interval time.Duration) bool {
	last := store.View().LastSync

	return last.IsZero() || time.Since(last) > interval
}

type garminUploader struct{}

func (garminUploader) Upload(ctx context.Context, accessToken string, tcx []byte, exerciseID string) (string, error) {
	return garmin.Upload(ctx, accessToken, tcx, exerciseID)
}
