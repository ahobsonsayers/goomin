package web

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/ahobsonsayers/health-connectd/api"
	"github.com/ahobsonsayers/health-connectd/frontend"
	"github.com/ahobsonsayers/health-connectd/garmin"
	"github.com/ahobsonsayers/health-connectd/google"
	"github.com/ahobsonsayers/health-connectd/state"
)

const (
	stateCookie = "oauth_state"
	loginFailed = "sign in again: the Garmin login session expired"
)

// Server holds the API state and the one in-flight Garmin login.
type Server struct {
	Store   *state.Store
	Google  *google.Client
	Trigger func()

	router chi.Router
	assets fs.FS
	mu     sync.Mutex
	login  *garmin.Login
	state  string
}

// New builds the router: JSON under /api, the React build everywhere else.
func New(store *state.Store, googleClient *google.Client, trigger func()) (*Server, error) {
	assets, err := frontend.FS()
	if err != nil {
		return nil, fmt.Errorf("read frontend build: %w", err)
	}

	server := &Server{
		Store:   store,
		Google:  googleClient,
		Trigger: trigger,
		assets:  assets,
	}

	router := chi.NewRouter()

	strictHandler := api.NewStrictHandler(server, nil)
	api.HandlerFromMux(strictHandler, router)
	router.Get("/*", server.page)
	server.router = router

	return server, nil
}

// ServeHTTP applies basic auth, then the routes.
func (s *Server) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	s.router.ServeHTTP(writer, request)
}

func (s *Server) page(writer http.ResponseWriter, request *http.Request) {
	cleanPath := path.Clean(request.URL.Path)
	name := strings.TrimPrefix(cleanPath, "/")

	if name == "" || name == "." {
		name = "index.html"
	}

	body, err := fs.ReadFile(s.assets, name)
	if err != nil {
		body, err = fs.ReadFile(s.assets, "index.html")
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

func (s *Server) GetStatus(
	_ context.Context,
	_ api.GetStatusRequestObject,
) (api.GetStatusResponseObject, error) {
	data := s.Store.View()

	lastSync := ""
	if !data.LastSync.IsZero() {
		lastSync = data.LastSync.Format(time.RFC3339)
	}

	return api.GetStatus200JSONResponse{
		Google:   data.Google.Connected(),
		Garmin:   data.Garmin.Connected(),
		GarminAs: data.GarminAs,
		LastSync: lastSync,
		Log:      append([]string{}, data.Log...),
	}, nil
}

func (s *Server) StartSync(_ context.Context, _ api.StartSyncRequestObject) (api.StartSyncResponseObject, error) {
	s.Trigger()

	return api.StartSync202JSONResponse{Message: "sync started"}, nil
}

func (s *Server) StartGoogle(
	_ context.Context,
	_ api.StartGoogleRequestObject,
) (api.StartGoogleResponseObject, error) {
	if s.Google.ClientID == "" || s.Google.ClientSecret == "" {
		return api.StartGoogle400JSONResponse{Message: "set GOOGLE_CLIENT_ID and GOOGLE_CLIENT_SECRET"}, nil
	}

	value := randomToken()

	s.mu.Lock()
	s.state = value
	s.mu.Unlock()

	return googleRedirect{location: s.Google.AuthURL(value), state: value}, nil
}

func (s *Server) FinishGoogle(
	ctx context.Context,
	request api.FinishGoogleRequestObject,
) (api.FinishGoogleResponseObject, error) {
	refusal := value(request.Params.Error)
	if refusal != "" {
		return back("google refused the sign-in: " + refusal), nil
	}

	if value(request.Params.State) != s.currentState() {
		return back("google returned to a different browser session"), nil
	}

	token, err := s.Google.Exchange(ctx, value(request.Params.Code))
	if err != nil {
		message := err.Error()

		return back(message), nil
	}

	err = s.Store.Update(func(data *state.Data) error {
		data.Google = token

		return nil
	})
	if err != nil {
		message := err.Error()

		return back(message), nil
	}

	s.Store.Logf("Google connected")

	return back("Google connected"), nil
}

func (s *Server) StartGarmin(
	ctx context.Context,
	_ api.StartGarminRequestObject,
) (api.StartGarminResponseObject, error) {
	login := garmin.NewLogin()

	err := login.Start(ctx)
	if err != nil {
		return api.StartGarmin502JSONResponse{Message: err.Error()}, nil
	}

	s.mu.Lock()
	s.login = login
	s.mu.Unlock()

	return api.StartGarmin202JSONResponse{Message: "ready"}, nil
}

func (s *Server) SignInGarmin(
	ctx context.Context,
	request api.SignInGarminRequestObject,
) (api.SignInGarminResponseObject, error) {
	if request.Body == nil {
		return api.SignInGarmin400JSONResponse{Message: "username and password are required"}, nil
	}

	login := s.currentLogin()
	if login == nil {
		return api.SignInGarmin400JSONResponse{Message: loginFailed}, nil
	}

	outcome, err := login.SignIn(ctx, request.Body.Username, request.Body.Password)
	if err != nil {
		return api.SignInGarmin400JSONResponse{Message: err.Error()}, nil
	}

	if outcome.NeedsMFA {
		return api.SignInGarmin200JSONResponse{Mfa: true}, nil
	}

	_, err = s.finishGarmin(ctx, outcome.Ticket)
	if err != nil {
		return api.SignInGarmin400JSONResponse{Message: err.Error()}, nil
	}

	return api.SignInGarmin200JSONResponse{Mfa: false}, nil
}

func (s *Server) SubmitGarminMFA(
	ctx context.Context,
	request api.SubmitGarminMFARequestObject,
) (api.SubmitGarminMFAResponseObject, error) {
	if request.Body == nil {
		return api.SubmitGarminMFA400JSONResponse{Message: "the code is required"}, nil
	}

	login := s.currentLogin()
	if login == nil {
		return api.SubmitGarminMFA400JSONResponse{Message: loginFailed}, nil
	}

	ticket, err := login.SubmitMFA(ctx, request.Body.Code)
	if err != nil {
		return api.SubmitGarminMFA400JSONResponse{Message: err.Error()}, nil
	}

	message, err := s.finishGarmin(ctx, ticket)
	if err != nil {
		return api.SubmitGarminMFA400JSONResponse{Message: err.Error()}, nil
	}

	return api.SubmitGarminMFA200JSONResponse{Message: message}, nil
}

func (s *Server) finishGarmin(ctx context.Context, ticket string) (string, error) {
	token, err := garmin.ExchangeTicket(ctx, ticket)
	if err != nil {
		return "", fmt.Errorf("exchange garmin ticket: %w", err)
	}

	name, err := garmin.DisplayName(ctx, token.AccessToken)
	if err != nil {
		s.Store.Logf("Garmin name unavailable: %v", err)
	}

	s.mu.Lock()
	s.login = nil
	s.mu.Unlock()

	err = s.Store.Update(func(data *state.Data) error {
		data.Garmin = token
		data.GarminAs = name

		return nil
	})
	if err != nil {
		return "", fmt.Errorf("save garmin token: %w", err)
	}

	s.Store.Logf("Garmin connected as %s", name)

	return "Garmin connected", nil
}

func (s *Server) Disconnect(
	_ context.Context,
	request api.DisconnectRequestObject,
) (api.DisconnectResponseObject, error) {
	provider := string(request.Provider)

	err := s.Store.Update(func(data *state.Data) error {
		switch provider {
		case "google":
			data.Google = state.Token{}
		case "garmin":
			data.Garmin = state.Token{}
			data.GarminAs = ""
		default:
			return errors.New("unknown provider")
		}

		return nil
	})
	if err != nil {
		return api.Disconnect400JSONResponse{Message: err.Error()}, nil
	}

	s.Store.Logf("%s disconnected", provider)

	return api.Disconnect200JSONResponse{Message: provider + " disconnected"}, nil
}

func (s *Server) currentLogin() *garmin.Login {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.login
}

func (s *Server) currentState() string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.state
}

func back(message string) api.FinishGoogle303Response {
	query := url.Values{"message": {message}}
	location := "/?" + query.Encode()

	return api.FinishGoogle303Response{
		Headers: api.FinishGoogle303ResponseHeaders{Location: &location},
	}
}

func randomToken() string {
	raw := make([]byte, 16)

	_, err := rand.Read(raw)
	if err != nil {
		return ""
	}

	return hex.EncodeToString(raw)
}

func value(pointer *string) string {
	if pointer == nil {
		return ""
	}

	return *pointer
}

// googleRedirect answers the consent redirect and remembers the state for the callback.
type googleRedirect struct {
	location, state string
}

func (g googleRedirect) VisitStartGoogleResponse(writer http.ResponseWriter) error {
	http.SetCookie(writer, &http.Cookie{ //nolint:exhaustruct // Defaults are correct for a short-lived cookie.
		Name:     stateCookie,
		Value:    g.state,
		Path:     "/",
		MaxAge:   int(time.Hour.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	writer.Header().Set("Location", g.location)
	writer.WriteHeader(http.StatusFound)

	return nil
}
