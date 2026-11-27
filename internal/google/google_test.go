package google

import (
	"context"
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
	require.Equal(t, "https://www.googleapis.com/auth/googlehealth.activity_and_fitness.readonly", query.Get("scope"))
}

func TestTokenError(t *testing.T) {
	t.Parallel()

	dead := tokenError("invalid_grant", "Token has been expired or revoked.")
	require.ErrorIs(t, dead, ErrGrantDead)

	config := tokenError("invalid_client", "Unauthorized")
	require.NotErrorIs(t, config, ErrGrantDead)
	require.ErrorContains(t, config, "invalid_client")
}

func TestDailySteps(t *testing.T) {
	t.Parallel()

	var authHeader, filter string

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		authHeader = request.Header.Get("Authorization")
		filter = request.URL.Query().Get("filter")

		_, _ = writer.Write([]byte(`{"dataPoints":[
			{"steps":{"count":"40","interval":{"civilStartTime":{"date":{"year":2026,"month":11,"day":4}}}}},
			{"steps":{"count":"60","interval":{"civilStartTime":{"date":{"year":2026,"month":11,"day":4}}}}},
			{"steps":{"count":"10","interval":{"civilStartTime":{"date":{"year":2026,"month":11,"day":5}}}}}
		]}`))
	}))
	defer server.Close()

	client := &Client{BaseURL: server.URL, HTTP: server.Client()}

	found, err := client.DailySteps(context.Background(), "token", time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC))
	require.NoError(t, err)
	require.Equal(t, "Bearer token", authHeader)
	require.Contains(t, filter, `steps.interval.civil_start_time >= "2026-11-01`)
	require.Equal(t, []StepDay{{Date: "2026-11-04", Steps: 100}, {Date: "2026-11-05", Steps: 10}}, found)
}
