package google

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCivil(t *testing.T) {
	t.Parallel()

	plusTwo := time.FixedZone("plus2", 2*60*60)
	require.Equal(t, "2026-11-25T10:00:00", Civil(time.Date(2026, 11, 25, 12, 0, 0, 0, plusTwo)))
}

func TestAuthURL(t *testing.T) {
	t.Parallel()

	client := &Client{ClientID: "id", RedirectURL: "https://example.com/callback"}

	parsed, err := url.Parse(client.AuthURL("st"))
	require.NoError(t, err)
	require.Equal(t, "https://accounts.google.com/o/oauth2/v2/auth", parsed.Scheme+"://"+parsed.Host+parsed.Path)

	query := parsed.Query()
	require.Equal(t, "id", query.Get("client_id"))
	require.Equal(t, "https://example.com/callback", query.Get("redirect_uri"))
	require.Equal(t, "offline", query.Get("access_type"))
	require.Equal(t, "consent", query.Get("prompt"))
	require.Equal(t, "st", query.Get("state"))
	require.Contains(t, query.Get("scope"), "googlehealth.location.readonly")
	require.Contains(t, query.Get("scope"), "googlehealth.activity_and_fitness.readonly")
}

func TestExercisesPages(t *testing.T) {
	t.Parallel()

	var authors, filters []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authors = append(authors, r.Header.Get("Authorization"))
		filters = append(filters, r.URL.Query().Get("filter"))

		if r.URL.Query().Get("pageToken") == "" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"dataPoints": []map[string]any{{
					"name":     "users/me/dataTypes/exercise/dataPoints/1",
					"exercise": map[string]any{"displayName": "Morning Run"},
				}},
				"nextPageToken": "next",
			})

			return
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"dataPoints": []map[string]any{{"name": "users/me/dataTypes/exercise/dataPoints/2"}},
		})
	}))
	defer server.Close()

	client := &Client{BaseURL: server.URL, HTTP: server.Client()}

	since := time.Date(2026, 11, 25, 0, 0, 0, 0, time.UTC)

	found, err := client.Exercises(context.Background(), "token", since)
	require.NoError(t, err)
	require.Len(t, found, 2)
	require.Equal(t, "1", found[0].ID)
	require.Equal(t, "Morning Run", found[0].Label)
	require.Equal(t, "2", found[1].ID)
	require.Equal(t, "exercise", found[1].Label)
	require.Equal(t, "Bearer token", authors[0])
	require.Equal(t, `exercise.interval.civil_start_time >= "2026-11-25T00:00:00"`, filters[0])
	require.Len(t, filters, 2)
}

func TestExportTCX(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("alt") != "media" {
			http.Error(w, "missing alt", http.StatusBadRequest)

			return
		}
		_, _ = w.Write([]byte("<TrainingCenterDatabase/>"))
	}))
	defer server.Close()

	client := &Client{BaseURL: server.URL, HTTP: server.Client()}

	tcx, err := client.ExportTCX(context.Background(), "token", "users/me/dataTypes/exercise/dataPoints/1")
	require.NoError(t, err)
	require.Equal(t, "<TrainingCenterDatabase/>", string(tcx))
}

func TestTokenError(t *testing.T) {
	t.Parallel()

	dead := tokenError("invalid_grant", "Token has been expired or revoked.")
	require.ErrorIs(t, dead, ErrGrantDead)

	config := tokenError("invalid_client", "Unauthorized")
	require.NotErrorIs(t, config, ErrGrantDead)
	require.ErrorContains(t, config, "invalid_client")
}
