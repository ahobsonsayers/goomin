package garmin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/ahobsonsayers/goomin/internal/state"
)

const (
	embedEndpoint    = "https://sso.garmin.com/sso/embed"
	mfaEndpoint      = "https://sso.garmin.com/sso/verifyMFA/loginEnterMfaCode"
	tokenEndpoint    = "https://diauth.garmin.com/di-oauth2-service/oauth/token"
	uploadEndpoint   = "https://connectapi.garmin.com/upload-service/upload/.tcx"
	settingsEndpoint = "https://connectapi.garmin.com/userprofile-service/userprofile/user-settings"
	activityEndpoint = "https://connectapi.garmin.com/activity-service/activity"

	mobileClientID = "GARMIN_CONNECT_MOBILE_ANDROID_DI"
	mobileAgent    = "GCM-Android-5.23"
	grantTicket    = "https://connectapi.garmin.com/di-oauth2-service/oauth/grant/service_ticket"
	importReferer  = "https://connect.garmin.com/modern/import-data"

	StatusUploaded = "uploaded"
	StatusAlready  = "already on Garmin"

	requestTimeout = time.Minute
	uploadTimeout  = 2 * time.Minute
)

var (
	csrfPattern     = regexp.MustCompile(`name="_csrf"\s+value="([^"]+)"`)
	responsePattern = regexp.MustCompile(`response_url\s*=\s*["']([^"']+)["']`)
	sportPattern    = regexp.MustCompile(`Sport="([^"]*)"`)
)

// Outcome is what one sign-in attempt produced: a ticket, or a request for the MFA code.
type Outcome struct {
	Ticket   string
	NeedsMFA bool
}

// Login holds the SSO cookies and csrf token for one sign-in.
type Login struct {
	HTTP     *http.Client
	EmbedURL string
	MFAURL   string
	csrf     string
}

// NewLogin starts a sign-in with its own cookie jar.
func NewLogin() *Login {
	jar, _ := cookiejar.New(nil)

	return &Login{HTTP: &http.Client{Jar: jar, Timeout: requestTimeout}}
}

// Start fetches Garmin's embed page and remembers its csrf token.
func (l *Login) Start(ctx context.Context) error {
	query := url.Values{
		"id":                              {"gauth-widget"},
		"embedWidget":                     {"true"},
		"clientId":                        {"GarminConnectMobileAndroid"},
		"locale":                          {"en"},
		"service":                         {"https://connect.garmin.com/app"},
		"gauthHost":                       {l.embed()},
		"redirectAfterAccountLoginUrl":    {"https://connect.garmin.com/app"},
		"redirectAfterAccountCreationUrl": {"https://connect.garmin.com/app"},
		"consumeServiceTicket":            {"true"},
	}

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		l.embed()+"?"+query.Encode(),
		http.NoBody,
	)
	if err != nil {
		return fmt.Errorf("build garmin sign-in request: %w", err)
	}

	body, err := l.do(request)
	if err != nil {
		return fmt.Errorf("load garmin sign-in page: %w", err)
	}

	match := csrfPattern.FindSubmatch(body)
	if match == nil {
		return errors.New("garmin sign-in page had no csrf token")
	}
	l.csrf = string(match[1])

	return nil
}

// SignIn posts the credentials and returns a ticket or a request for the MFA code.
func (l *Login) SignIn(ctx context.Context, username, password string) (Outcome, error) {
	form := url.Values{
		"username": {username},
		"password": {password},
		"embed":    {"true"},
		"_csrf":    {l.csrf},
	}

	body, err := l.post(ctx, l.embed(), form)
	if err != nil {
		return Outcome{}, fmt.Errorf("garmin sign-in: %w", err)
	}

	outcome := parseOutcome(body)
	if outcome.Ticket != "" || outcome.NeedsMFA {
		return outcome, nil
	}

	return Outcome{}, fmt.Errorf("garmin refused the sign-in: %s", heading(body))
}

// SubmitMFA posts the security code and returns the ticket.
func (l *Login) SubmitMFA(ctx context.Context, code string) (string, error) {
	form := url.Values{
		"mfa-code": {code},
		"embed":    {"true"},
		"_csrf":    {l.csrf},
	}

	body, err := l.post(ctx, l.mfa(), form)
	if err != nil {
		return "", fmt.Errorf("garmin mfa: %w", err)
	}

	outcome := parseOutcome(body)
	if outcome.Ticket != "" {
		return outcome.Ticket, nil
	}

	return "", fmt.Errorf("garmin refused the security code: %s", heading(body))
}

// ExchangeTicket trades an SSO ticket for Garmin tokens.
func ExchangeTicket(ctx context.Context, ticket string) (state.Token, error) {
	form := url.Values{
		"grant_type":     {grantTicket},
		"service_ticket": {ticket},
	}

	return tokenRequest(ctx, form)
}

// Refresh trades a Garmin refresh token for a new access token.
func Refresh(ctx context.Context, refreshToken string) (state.Token, error) {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	}

	return tokenRequest(ctx, form)
}

// DisplayName reads the account name shown on Garmin Connect.
func DisplayName(ctx context.Context, accessToken string) (string, error) {
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		settingsEndpoint,
		http.NoBody,
	)
	if err != nil {
		return "", fmt.Errorf("build garmin settings request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+accessToken)
	request.Header.Set("User-Agent", mobileAgent)

	response, err := defaultClient().Do(request)
	if err != nil {
		return "", fmt.Errorf("garmin settings: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode == http.StatusUnauthorized {
		return "", ErrNotSignedIn
	}

	var body settingsResponse

	decoder := json.NewDecoder(response.Body)

	err = decoder.Decode(&body)
	if err != nil {
		return "", fmt.Errorf("parse garmin settings: %w", err)
	}

	if body.UserData.DisplayName != "" {
		return body.UserData.DisplayName, nil
	}

	return body.UserData.FullName, nil
}

type userSettings struct {
	DisplayName string `json:"displayName"`
	FullName    string `json:"fullName"`
}

type settingsResponse struct {
	UserData userSettings `json:"userData"`
}

// ErrNotSignedIn means Garmin rejected the access token.
var ErrNotSignedIn = errors.New("garmin: sign-in has expired")

// Upload sends one TCX and returns a one-line status for the log.
func Upload(ctx context.Context, accessToken string, tcx []byte, exerciseID string) (string, error) {
	status, body, err := uploadFile(ctx, accessToken, tcx, exerciseID+".tcx")
	if err != nil {
		return "", err
	}

	return UploadStatus(status, body), nil
}

// UploadSteps imports one day's step count as an Other activity, then names it
// after the count — Garmin discards step fields embedded in activity files.
// Days sync at most once: the syncer records "steps-<date>" when UploadStatus
// reports the import succeeded.
func UploadSteps(ctx context.Context, accessToken, date string, steps int) (string, error) {
	status, body, err := uploadFile(ctx, accessToken, stepsTCX(date, steps), "steps-"+date+".tcx")
	if err != nil {
		return "", err
	}

	statusText := UploadStatus(status, body)
	if statusText != StatusUploaded {
		return statusText, nil
	}

	activityID, found := internalID(body)
	if !found {
		return statusText, nil
	}

	name := fmt.Sprintf("Walk · %d steps · %s", steps, date)

	err = renameActivity(ctx, accessToken, activityID, name)
	if err != nil {
		return "", fmt.Errorf("rename garmin steps activity: %w", err)
	}

	return statusText, nil
}

func uploadFile(
	ctx context.Context,
	accessToken string,
	content []byte,
	filename string,
) (statusCode int, responseBody []byte, err error) {
	var payload bytes.Buffer

	writer := multipart.NewWriter(&payload)

	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return 0, nil, fmt.Errorf("create garmin upload form: %w", err)
	}

	rewritten := RewriteSport(content)

	_, err = part.Write(rewritten)
	if err != nil {
		return 0, nil, fmt.Errorf("write garmin upload form: %w", err)
	}

	err = writer.Close()
	if err != nil {
		return 0, nil, fmt.Errorf("close garmin upload form: %w", err)
	}

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		uploadEndpoint,
		&payload,
	)
	if err != nil {
		return 0, nil, fmt.Errorf("build garmin upload request: %w", err)
	}

	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("Authorization", "Bearer "+accessToken)
	request.Header.Set("Referer", importReferer)
	request.Header.Set("User-Agent", mobileAgent)

	response, err := defaultClient().Do(request)
	if err != nil {
		return 0, nil, fmt.Errorf("garmin upload: %w", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return 0, nil, fmt.Errorf("read garmin upload response: %w", err)
	}

	return response.StatusCode, body, nil
}

// stepsTCX builds the smallest TCX file the importer accepts for a day's steps.
func stepsTCX(date string, steps int) []byte {
	start := date + "T00:00:00Z"

	return []byte(fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8" standalone="no"?>
<TrainingCenterDatabase xmlns="http://www.garmin.com/xmlschemas/TrainingCenterDatabase/v2">
  <Activities>
    <Activity Sport="Other">
      <Id>%s</Id>
      <Notes>%d steps by goomin</Notes>
      <Lap StartTime="%s">
        <TotalTimeSeconds>60</TotalTimeSeconds>
        <DistanceMeters>0.0</DistanceMeters>
        <Calories>0</Calories>
        <Intensity>Active</Intensity>
        <TriggerMethod>Manual</TriggerMethod>
      </Lap>
    </Activity>
  </Activities>
  <Author>
    <Name>goomin</Name>
  </Author>
</TrainingCenterDatabase>
`, start, steps, start))
}

func internalID(body []byte) (string, bool) {
	var decoded importResult

	err := json.Unmarshal(body, &decoded)
	if err != nil {
		return "", false
	}

	for _, success := range decoded.Result.Successes {
		return fmt.Sprint(success.ID), true
	}

	return "", false
}

func renameActivity(ctx context.Context, accessToken, activityID, name string) error {
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPut,
		activityEndpoint+"/"+activityID,
		strings.NewReader(`{"activityName":"`+name+`"}`),
	)
	if err != nil {
		return fmt.Errorf("build garmin rename request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+accessToken)

	response, err := defaultClient().Do(request)
	if err != nil {
		return fmt.Errorf("garmin rename: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode >= http.StatusBadRequest {
		return fmt.Errorf("garmin rename returned %d", response.StatusCode)
	}

	return nil
}

// RewriteSport forces the sport Garmin accepts; Google exports walking and swimming.
func RewriteSport(tcx []byte) []byte {
	return sportPattern.ReplaceAllFunc(tcx, func(match []byte) []byte {
		sport := sportPattern.FindSubmatch(match)[1]
		switch string(sport) {
		case "Running", "Biking", "Other":
			return match
		default:
			return []byte(`Sport="Running"`)
		}
	})
}

// UploadStatus turns Garmin's response into one line for the log. Never fails.
func UploadStatus(status int, body []byte) string {
	if status == http.StatusConflict {
		return StatusAlready
	}

	failure := firstFailure(body)
	if failure != "" {
		return "Garmin refused it: " + failure
	}

	if status != http.StatusOK && status != http.StatusCreated {
		return fmt.Sprintf("upload failed (%d)", status)
	}

	return StatusUploaded
}

// ticketFromURL pulls the SSO ticket out of the URL Garmin redirects to.
func ticketFromURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}

	return parsed.Query().Get("ticket")
}

func parseOutcome(body []byte) Outcome {
	match := responsePattern.FindSubmatch(body)
	if match != nil {
		ticket := ticketFromURL(string(match[1]))

		return Outcome{Ticket: ticket}
	}

	if bytes.Contains(body, []byte("verifyMFA")) {
		return Outcome{NeedsMFA: true}
	}

	return Outcome{}
}

type importSuccess struct {
	ID any `json:"internalId"`
}

type importFailures struct {
	Failures  []map[string]any `json:"failures"`
	Successes []importSuccess  `json:"successes"`
}

type importResult struct {
	Result importFailures `json:"detailedImportResult"`
}

func firstFailure(body []byte) string {
	decoded := importResult{}

	err := json.Unmarshal(body, &decoded)
	if err != nil || len(decoded.Result.Failures) == 0 {
		return ""
	}

	first := decoded.Result.Failures[0]

	for _, key := range []string{"messages", "message"} {
		message, ok := first[key]
		if ok {
			return fmt.Sprint(message)
		}
	}

	return fmt.Sprint(first)
}

func tokenRequest(ctx context.Context, form url.Values) (state.Token, error) {
	form.Set("client_id", mobileClientID)

	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		tokenEndpoint,
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		return state.Token{}, fmt.Errorf("build garmin token request: %w", err)
	}
	request.SetBasicAuth(mobileClientID, "")
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("User-Agent", mobileAgent)

	response, err := defaultClient().Do(request)
	if err != nil {
		return state.Token{}, fmt.Errorf("garmin token request: %w", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return state.Token{}, fmt.Errorf("read garmin token response: %w", err)
	}

	return tokenFromResponse(body)
}

func tokenFromResponse(body []byte) (state.Token, error) {
	decoded := struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		Error        string `json:"error"`
		Message      string `json:"message"`
	}{}

	err := json.Unmarshal(body, &decoded)
	if err != nil {
		return state.Token{}, fmt.Errorf("parse garmin token response: %w", err)
	}

	if decoded.AccessToken == "" {
		return state.Token{}, fmt.Errorf("garmin returned no access token: %s %s", decoded.Error, decoded.Message)
	}

	return state.Token{
		AccessToken:  decoded.AccessToken,
		RefreshToken: decoded.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(decoded.ExpiresIn) * time.Second),
	}, nil
}

func (l *Login) post(ctx context.Context, endpoint string, form url.Values) ([]byte, error) {
	request, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		endpoint,
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		return nil, fmt.Errorf("build garmin form request: %w", err)
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Origin", "null")

	return l.do(request)
}

func (l *Login) do(request *http.Request) ([]byte, error) {
	response, err := l.HTTP.Do(request)
	if err != nil {
		return nil, fmt.Errorf("garmin request: %w", err)
	}
	defer response.Body.Close()

	return io.ReadAll(response.Body)
}

func (l *Login) embed() string {
	if l.EmbedURL != "" {
		return l.EmbedURL
	}

	return embedEndpoint
}

func (l *Login) mfa() string {
	if l.MFAURL != "" {
		return l.MFAURL
	}

	return mfaEndpoint
}

func defaultClient() *http.Client { return &http.Client{Timeout: uploadTimeout} }

func heading(body []byte) string {
	text := strings.TrimSpace(string(body))
	if len(text) > 200 {
		return text[:200]
	}

	return text
}
