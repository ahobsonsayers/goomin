package web

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ahobsonsayers/health-connectd/api"
	"github.com/ahobsonsayers/health-connectd/google"
	"github.com/ahobsonsayers/health-connectd/state"
)

func TestStatusReportsTheStore(t *testing.T) {
	t.Parallel()

	server := newServer(t, "", nil)

	recorder := get(server, "/api/status")
	require.Equal(t, http.StatusOK, recorder.Code)

	var status api.Status
	require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &status))
	require.False(t, status.Google)
	require.False(t, status.Garmin)
	require.Empty(t, status.LastSync)
}

func TestSyncStartsTheTrigger(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	server := newServer(t, "", func() { calls.Add(1) })

	recorder := post(server, "/api/sync")
	require.Equal(t, http.StatusAccepted, recorder.Code)
	require.EqualValues(t, 1, calls.Load())
}

func TestDisconnectRejectsUnknownProvider(t *testing.T) {
	t.Parallel()

	server := newServer(t, "", nil)

	recorder := post(server, "/api/disconnect/garmin")
	require.Equal(t, http.StatusOK, recorder.Code)

	recorder = post(server, "/api/disconnect/polar")
	require.Equal(t, http.StatusBadRequest, recorder.Code)
}

func TestPasswordGuardsEverything(t *testing.T) {
	t.Parallel()

	server := newServer(t, "secret", nil)

	anonymous := get(server, "/api/status")
	require.Equal(t, http.StatusUnauthorized, anonymous.Code)

	request := httptest.NewRequest(http.MethodGet, "/api/status", http.NoBody)
	request.SetBasicAuth("me", "secret")
	allowed := httptest.NewRecorder()
	server.ServeHTTP(allowed, request)
	require.Equal(t, http.StatusOK, allowed.Code)
}

func TestPageServesTheBuild(t *testing.T) {
	t.Parallel()

	server := newServer(t, "", nil)

	if _, err := fs.ReadFile(server.assets, "index.html"); err != nil {
		t.Skip("run task build:fe to test spa serving")
	}

	recorder := get(server, "/")
	require.Equal(t, http.StatusOK, recorder.Code)
	require.Contains(t, recorder.Body.String(), "<div id=\"root\">")

	unknown := get(server, "/sessions/42")
	require.Equal(t, http.StatusOK, unknown.Code)
	require.Contains(t, unknown.Body.String(), "<div id=\"root\">")
}

func newServer(t *testing.T, password string, trigger func()) *Server {
	t.Helper()

	store, err := state.Open(filepath.Join(t.TempDir(), "state.json"))
	require.NoError(t, err)

	if trigger == nil {
		trigger = func() {}
	}

	server, err := New(store, &google.Client{}, trigger, password)
	require.NoError(t, err)

	return server
}

func get(server *Server, target string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, http.NoBody))

	return recorder
}

func post(server *Server, target string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, target, http.NoBody)

	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, request)

	return recorder
}
