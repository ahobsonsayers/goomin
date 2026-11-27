package server

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"github.com/ahobsonsayers/goomin/google"
	"github.com/ahobsonsayers/goomin/state"
)

func TestStatusReportsTheStore(t *testing.T) {
	t.Parallel()

	router := testRouter(t, nil, nil)

	recorder := get(router, "/api/status")
	require.Equal(t, http.StatusOK, recorder.Code)

	var status Status
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &status))
	require.False(t, status.Google)
	require.False(t, status.Garmin)
	require.Empty(t, status.LastSync)
	require.Empty(t, status.Log)
}

func TestSyncStartsTheTrigger(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32

	recorder := post(testRouter(t, func() { calls.Add(1) }, nil), "/api/sync")
	require.Equal(t, http.StatusAccepted, recorder.Code)
	require.EqualValues(t, 1, calls.Load())
}

func TestDisconnectRejectsUnknownProvider(t *testing.T) {
	t.Parallel()

	router := testRouter(t, nil, nil)

	recorder := post(router, "/api/disconnect/garmin")
	require.Equal(t, http.StatusOK, recorder.Code)

	recorder = post(router, "/api/disconnect/polar")
	require.Equal(t, http.StatusBadRequest, recorder.Code)
}

func TestPageServesTheBuild(t *testing.T) {
	t.Parallel()

	router := testRouter(t, nil, frontendFS())

	recorder := get(router, "/")
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), "<div id=\"root\">")

	recorder = get(router, "/assets/app.js")
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), "app")

	recorder = get(router, "/anything")
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Equal(t, []byte("<div id=\"root\">"), recorder.Body.Bytes())
}

func TestGarminSignInNeedsASession(t *testing.T) {
	t.Parallel()

	router := testRouter(t, nil, nil)

	recorder := postJSON(router, "/api/auth/garmin", `{"username":"a@example.com","password":"hunter2"}`)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Contains(t, recorder.Body.String(), "the Garmin login session expired")

	recorder = postJSON(router, "/api/auth/garmin/mfa", `{"code":"123456"}`)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
	require.Contains(t, recorder.Body.String(), "the Garmin login session expired")
}

func TestGoogleCallbackRejectsAStrangerState(t *testing.T) {
	t.Parallel()

	store, err := state.Open(filepath.Join(t.TempDir(), "state.json"))
	require.NoError(t, err)

	apiServer := NewServer(store, &google.Client{ClientID: "id", ClientSecret: "s"}, func() {})
	handler, err := newRouter(apiServer, frontendFS())
	require.NoError(t, err)

	start := get(handler, "/api/auth/google")
	require.Equal(t, http.StatusFound, start.Code)

	var stateCookie *http.Cookie
	for _, cookie := range start.Result().Cookies() {
		if cookie.Name == "oauth_state" {
			stateCookie = cookie
		}
	}
	require.NotNil(t, stateCookie)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/auth/google/callback?code=c&state=tampered", http.NoBody)
	request.AddCookie(stateCookie)
	handler.ServeHTTP(recorder, request)
	require.Equal(t, http.StatusSeeOther, recorder.Code)
	require.Contains(t, recorder.Header().Get("Location"), "different+browser+session")
	require.False(t, store.View().Google.Connected())
}

func testRouter(t *testing.T, trigger func(), assets fs.FS) http.Handler {
	t.Helper()

	if trigger == nil {
		trigger = func() {}
	}
	if assets == nil {
		assets = frontendFS()
	}

	store, err := state.Open(filepath.Join(t.TempDir(), "state.json"))
	require.NoError(t, err)

	handler, err := newRouter(NewServer(store, &google.Client{}, trigger), assets)
	require.NoError(t, err)

	return handler
}

func frontendFS() fs.FS {
	return fstest.MapFS{
		"index.html":    {Data: []byte("<div id=\"root\">")},
		"assets/app.js": {Data: []byte("app")},
	}
}

func get(router http.Handler, target string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, http.NoBody))

	return recorder
}

func post(router http.Handler, target string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, target, http.NoBody))

	return recorder
}

func postJSON(router http.Handler, target, body string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(recorder, request)

	return recorder
}
