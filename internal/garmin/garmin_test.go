package garmin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestTicketFromURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		url  string
		want string
	}{
		{name: "ticket", url: "https://connect.garmin.com/app?ticket=ST-1", want: "ST-1"},
		{name: "ticket among others", url: "https://connect.garmin.com/app?a=1&ticket=ST-2&b=2", want: "ST-2"},
		{name: "no ticket", url: "https://connect.garmin.com/app", want: ""},
		{name: "not a url", url: "://", want: ""},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, test.want, ticketFromURL(test.url))
		})
	}
}

func TestRewriteSport(t *testing.T) {
	t.Parallel()

	require.Equal(t, `Sport="Running"`, string(RewriteSport([]byte(`Sport="Walking"`))))
	require.Equal(t, `Sport="Running"`, string(RewriteSport([]byte(`Sport="Swimming"`))))
	require.Equal(t, `Sport="Biking"`, string(RewriteSport([]byte(`Sport="Biking"`))))
	require.Equal(t, `Sport="Other"`, string(RewriteSport([]byte(`Sport="Other"`))))
}

func TestUploadStatus(t *testing.T) {
	t.Parallel()

	failure := []byte(`{"detailedImportResult":{"failures":[{"messages":"Bad file"}]}}`)
	message := []byte(`{"detailedImportResult":{"failures":[{"message":"Empty activity"}]}}`)

	tests := []struct {
		name   string
		status int
		body   []byte
		want   string
	}{
		{name: "conflict", status: http.StatusConflict, want: StatusAlready},
		{
			name:   "ok",
			status: http.StatusOK,
			body:   []byte(`{"detailedImportResult":{"successes":[]}}`),
			want:   StatusUploaded,
		},
		{name: "created", status: http.StatusCreated, want: StatusUploaded},
		{name: "refused", status: http.StatusBadRequest, body: failure, want: "Garmin refused it: Bad file"},
		{
			name:   "refused by message",
			status: http.StatusBadRequest,
			body:   message,
			want:   "Garmin refused it: Empty activity",
		},
		{
			name:   "html gateway",
			status: http.StatusBadGateway,
			body:   []byte("<html>oops</html>"),
			want:   "upload failed (502)",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, test.want, UploadStatus(test.status, test.body))
		})
	}
}

func TestParseOutcome(t *testing.T) {
	t.Parallel()

	double := []byte(`<script>var response_url = "https://connect.garmin.com/app?ticket=ST-9";</script>`)
	single := []byte(`<script>var response_url = 'https://connect.garmin.com/app?ticket=ST-8';</script>`)

	require.Equal(t, "ST-9", parseOutcome(double).Ticket)
	require.Equal(t, "ST-8", parseOutcome(single).Ticket)
	require.True(t, parseOutcome([]byte(`<form action="/sso/verifyMFA/loginEnterMfaCode">`)).NeedsMFA)
	require.False(t, parseOutcome([]byte(`<p>wrong password</p>`)).NeedsMFA)
}

func TestLoginFlow(t *testing.T) {
	t.Parallel()

	signed := ""

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/mfa" {
			mfa := `<script>var response_url = "https://connect.garmin.com/app?ticket=ST-MFA";</script>`
			_, _ = w.Write([]byte(mfa))

			return
		}

		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`<input type="hidden" name="_csrf" value="tok123">`))

			return
		}

		_ = r.ParseForm()
		signed = r.PostForm.Get("username") + ":" + r.PostForm.Get("password") + ":" + r.PostForm.Get("_csrf")
		_, _ = w.Write([]byte(`<script>var response_url = "https://connect.garmin.com/app?ticket=ST-42";</script>`))
	}))
	defer server.Close()

	login := &Login{HTTP: server.Client(), EmbedURL: server.URL + "/embed", MFAURL: server.URL + "/mfa"}

	require.NoError(t, login.Start(context.Background()))

	outcome, err := login.SignIn(context.Background(), "me@example.com", "hunter2")
	require.NoError(t, err)
	require.Equal(t, "ST-42", outcome.Ticket)
	require.Equal(t, "me@example.com:hunter2:tok123", signed)

	ticket, err := login.SubmitMFA(context.Background(), "123456")
	require.NoError(t, err)
	require.Equal(t, "ST-MFA", ticket)
}

func TestTokenFromResponse(t *testing.T) {
	t.Parallel()

	token, err := tokenFromResponse([]byte(`{"access_token":"a","refresh_token":"r","expires_in":3600}`))
	require.NoError(t, err)
	require.Equal(t, "a", token.AccessToken)
	require.Equal(t, "r", token.RefreshToken)
	require.Greater(t, time.Until(token.ExpiresAt), 59*time.Minute)

	_, err = tokenFromResponse([]byte(`{"error":"invalid_grant","message":"bad ticket"}`))
	require.ErrorContains(t, err, "invalid_grant")

	_, err = tokenFromResponse([]byte(`not json`))
	require.Error(t, err)
}
