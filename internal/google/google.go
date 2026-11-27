package google

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ahobsonsayers/goomin/internal/state"
)

const (
	authEndpoint   = "https://accounts.google.com/o/oauth2/v2/auth"
	tokenEndpoint  = "https://oauth2.googleapis.com/token"
	healthAPI      = "https://health.googleapis.com/v4"
	stepsCap       = 10000
	requestTimeout = time.Minute
)

// scopes grant the step data the sync reads.
var scopes = []string{
	"https://www.googleapis.com/auth/googlehealth.activity_and_fitness.readonly",
}

// ErrGrantDead means the refresh token is gone for good, not a temporary failure.
var ErrGrantDead = errors.New("google: sign-in has expired")

// StepDay is the step total for one civil day, local to the recording device.
type StepDay struct {
	Date  string // YYYY-MM-DD
	Steps int
}

// Client holds the OAuth client settings and the HTTP client used for calls.
type Client struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
	BaseURL      string
	HTTP         *http.Client
}

// AuthURL is the consent page for the given state value.
func (c *Client) AuthURL(stateValue string) string {
	query := url.Values{
		"client_id":     {c.ClientID},
		"redirect_uri":  {c.RedirectURL},
		"response_type": {"code"},
		"scope":         {strings.Join(scopes, " ")},
		"access_type":   {"offline"},
		"prompt":        {"consent"},
		"state":         {stateValue},
	}

	return authEndpoint + "?" + query.Encode()
}

// Exchange trades an authorization code for tokens.
func (c *Client) Exchange(ctx context.Context, code string) (state.Token, error) {
	form := url.Values{
		"grant_type":   {"authorization_code"},
		"code":         {code},
		"redirect_uri": {c.RedirectURL},
	}

	return c.postToken(ctx, form, "")
}

// Refresh trades a refresh token for a new access token.
func (c *Client) Refresh(ctx context.Context, refreshToken string) (state.Token, error) {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	}

	return c.postToken(ctx, form, refreshToken)
}

// Civil is the offset-free timestamp the exercise filter compares against.
func Civil(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05") }

type stepPage struct {
	DataPoints    []stepPoint `json:"dataPoints"`
	NextPageToken string      `json:"nextPageToken"`
}

type stepPoint struct {
	Steps stepRecord `json:"steps"`
}

type stepRecord struct {
	Count    string       `json:"count"`
	Interval stepInterval `json:"interval"`
}

type stepInterval struct {
	CivilStartTime civilTime `json:"civilStartTime"`
}

type civilTime struct {
	Date civilDate `json:"date"`
}

type civilDate struct {
	Year  int `json:"year"`
	Month int `json:"month"`
	Day   int `json:"day"`
}

func (d civilDate) String() string {
	return fmt.Sprintf("%04d-%02d-%02d", d.Year, d.Month, d.Day)
}

func (c *Client) postToken(ctx context.Context, form url.Values, keepRefresh string) (state.Token, error) {
	form.Set("client_id", c.ClientID)
	form.Set("client_secret", c.ClientSecret)

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		tokenEndpoint,
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		return state.Token{}, fmt.Errorf("build google token request: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	response, err := c.client().Do(request)
	if err != nil {
		return state.Token{}, fmt.Errorf("google token request: %w", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return state.Token{}, fmt.Errorf("read google token response: %w", err)
	}

	decoded := tokenResponse{}

	err = json.Unmarshal(body, &decoded)
	if err != nil {
		return state.Token{}, fmt.Errorf("parse google token response: %w", err)
	}

	if decoded.Error != "" {
		return state.Token{}, tokenError(decoded.Error, decoded.ErrorDescription)
	}
	if decoded.AccessToken == "" {
		return state.Token{}, errors.New("google returned no access token")
	}

	refresh := decoded.RefreshToken
	if refresh == "" {
		refresh = keepRefresh
	}

	return state.Token{
		AccessToken:  decoded.AccessToken,
		RefreshToken: refresh,
		ExpiresAt:    time.Now().Add(time.Duration(decoded.ExpiresIn) * time.Second),
	}, nil
}

func (c *Client) getJSON(ctx context.Context, accessToken, endpoint string, into any) error {
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		endpoint,
		http.NoBody,
	)
	if err != nil {
		return fmt.Errorf("build google health request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+accessToken)

	response, err := c.client().Do(request)
	if err != nil {
		return fmt.Errorf("google health request: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(response.Body)

		return fmt.Errorf("google health returned %d: %s", response.StatusCode, snippet(body))
	}

	decoder := json.NewDecoder(response.Body)

	err = decoder.Decode(into)
	if err != nil {
		return fmt.Errorf("parse google health response: %w", err)
	}

	return nil
}

type tokenResponse struct {
	AccessToken      string `json:"access_token"`
	RefreshToken     string `json:"refresh_token"`
	ExpiresIn        int    `json:"expires_in"`
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

// DailySteps totals the recorded steps per civil day since the given date.
// The list endpoint only accepts >= filters, so the window grows every run and the syncer trims it with the ledger.
func (c *Client) DailySteps(ctx context.Context, accessToken string, since time.Time) ([]StepDay, error) {
	dayTotals := map[string]int{}
	var orderedDays []string

	pageToken := ""
	for {
		query := url.Values{
			"filter":   {`steps.interval.civil_start_time >= "` + Civil(since) + `"`},
			"pageSize": {fmt.Sprint(stepsCap)},
		}
		if pageToken != "" {
			query.Set("pageToken", pageToken)
		}

		var page stepPage

		endpoint := c.base() + "/users/me/dataTypes/steps/dataPoints?" + query.Encode()

		err := c.getJSON(ctx, accessToken, endpoint, &page)
		if err != nil {
			return nil, fmt.Errorf("list steps: %w", err)
		}

		for _, point := range page.DataPoints {
			count, err := strconv.Atoi(point.Steps.Count)
			if err != nil || count <= 0 {
				continue
			}

			date := point.Steps.Interval.CivilStartTime.Date.String()

			if _, seen := dayTotals[date]; !seen {
				orderedDays = append(orderedDays, date)
			}
			dayTotals[date] += count
		}

		pageToken = page.NextPageToken
		if pageToken == "" {
			break
		}
	}

	slices.Sort(orderedDays)

	totals := make([]StepDay, 0, len(orderedDays))

	for _, date := range orderedDays {
		totals = append(totals, StepDay{Date: date, Steps: dayTotals[date]})
	}

	return totals, nil
}

func tokenError(code, description string) error {
	if code == "invalid_grant" {
		return fmt.Errorf("%w: %s", ErrGrantDead, description)
	}

	return fmt.Errorf("google refused the token request: %s: %s", code, description)
}

func (c *Client) client() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}

	return &http.Client{Timeout: requestTimeout}
}

func (c *Client) base() string {
	if c.BaseURL != "" {
		return strings.TrimSuffix(c.BaseURL, "/")
	}

	return healthAPI
}

func snippet(body []byte) string {
	text := strings.TrimSpace(string(body))
	if len(text) > 200 {
		return text[:200]
	}

	return text
}
