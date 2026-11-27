package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/httplog/v2"

	oapimiddleware "github.com/oapi-codegen/nethttp-middleware"
)

const (
	readHeaderTimeout = 5 * time.Second
	shutdownTimeout   = 10 * time.Second
)

// Start runs the server until the context is cancelled.
func Start(ctx context.Context, port int, apiServer ServerInterface, frontendFS fs.FS) error {
	router, err := newRouter(apiServer, frontendFS)
	if err != nil {
		return fmt.Errorf("build router: %w", err)
	}

	address := fmt.Sprintf(":%d", port)
	httpServer := &http.Server{
		Addr:              address,
		Handler:           router,
		ReadHeaderTimeout: readHeaderTimeout,
	}

	serverErr := make(chan error, 1)
	go func() {
		serverErr <- httpServer.ListenAndServe()
	}()

	slog.Info("Server listening", "address", httpServer.Addr)

	select {
	case err := <-serverErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}

		return fmt.Errorf("server stopped: %w", err)
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()

		err = httpServer.Shutdown(shutdownCtx)
		if err != nil {
			return fmt.Errorf("shut down server: %w", err)
		}

		return nil
	}
}

func newRouter(apiServer ServerInterface, frontendFS fs.FS) (http.Handler, error) {
	validate, err := createOapiValidationMiddleware()
	if err != nil {
		return nil, fmt.Errorf("build openapi validation middleware: %w", err)
	}

	apiRouter := chi.NewRouter()
	apiRouter.Use(validate)
	apiHandler := HandlerFromMux(apiServer, apiRouter)

	router := chi.NewRouter()
	router.Use(createLoggerMiddleware())
	router.Mount("/api", apiHandler)
	router.Get("/*", page(frontendFS))

	return router, nil
}

func createLoggerMiddleware() func(http.Handler) http.Handler {
	logger := httplog.NewLogger(
		"health-connectd",
		httplog.Options{
			LogLevel:       slog.LevelDebug,
			RequestHeaders: true,
			Concise:        true,
		},
	)

	// This middleware includes the recover middleware
	return httplog.RequestLogger(logger)
}

func createOapiValidationMiddleware() (func(http.Handler) http.Handler, error) {
	spec, err := GetSpec()
	if err != nil {
		return nil, fmt.Errorf("load openapi spec: %w", err)
	}

	// Set server endpoint to the route so request validation matches the mount path.
	// See: https://github.com/oapi-codegen/oapi-codegen/issues/1123
	spec.Servers = openapi3.Servers{{URL: "/api"}}

	return oapimiddleware.OapiRequestValidatorWithOptions(
		spec,
		&oapimiddleware.Options{
			SilenceServersWarning: true,
			Options: openapi3filter.Options{
				AuthenticationFunc: func(_ context.Context, _ *openapi3filter.AuthenticationInput) error {
					return nil
				},
			},
		},
	), nil
}

// page serves the React build, with index.html as the fallback for frontend routes.
func page(assets fs.FS) func(http.ResponseWriter, *http.Request) {
	return func(writer http.ResponseWriter, request *http.Request) {
		cleanPath := path.Clean(request.URL.Path)
		name := strings.TrimPrefix(cleanPath, "/")

		if name == "" || name == "." {
			name = "index.html"
		}

		body, err := fs.ReadFile(assets, name)
		if err != nil {
			body, err = fs.ReadFile(assets, "index.html")
			if err != nil {
				http.NotFound(writer, request)

				return
			}
			name = "index.html"
		}

		content := bytes.NewReader(body)

		http.ServeContent(
			writer,
			request,
			name,
			time.Time{},
			content,
		)
	}
}
