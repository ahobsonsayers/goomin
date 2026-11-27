package server

import (
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"

	"github.com/ahobsonsayers/health-connectd/google"
	"github.com/ahobsonsayers/health-connectd/state"
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
