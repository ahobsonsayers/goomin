package syncer

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ahobsonsayers/health-connectd/garmin"
	"github.com/ahobsonsayers/health-connectd/google"
	"github.com/ahobsonsayers/health-connectd/state"
)

// DefaultWindow is wider than the 12 h interval: a weekly Google re-consent must not drop a workout.
const DefaultWindow = 90 * 24 * time.Hour

// Health is the Google side, kept as an interface so tests can replace it.
type Health interface {
	Exercises(ctx context.Context, accessToken string, since time.Time) ([]google.Exercise, error)
	ExportTCX(ctx context.Context, accessToken, name string) ([]byte, error)
}

// Uploader is the Garmin side.
type Uploader interface {
	Upload(ctx context.Context, accessToken string, tcx []byte, exerciseID string) (string, error)
}

// Tokens returns a usable access token, renewing it when force is set.
type Tokens func(ctx context.Context, force bool) (state.Token, error)

// Sync is one Google-to-Garmin run.
type Sync struct {
	Store    *state.Store
	Health   Health
	Uploader Uploader
	Google   Tokens
	Garmin   Tokens
	Window   time.Duration

	running atomic.Bool
}

// Run uploads every exercise that is not in the ledger yet.
func (s *Sync) Run(ctx context.Context) error {
	if !s.running.CompareAndSwap(false, true) {
		s.Store.Logf("a sync is already running")

		return nil
	}
	defer s.running.Store(false)

	since := time.Now().Add(-s.window())
	s.Store.Logf("Google Health, since %s", since.Format("2006-01-02"))

	googleToken, err := s.Google(ctx, false)
	if err != nil {
		s.Store.Logf("  Google: %v", err)

		return fmt.Errorf("google token: %w", err)
	}

	exercises, err := s.Health.Exercises(ctx, googleToken.AccessToken, since)
	if err != nil {
		s.Store.Logf("  %v", err)

		return fmt.Errorf("list exercises: %w", err)
	}

	pending := Pending(exercises, s.Store.View().Uploaded)
	s.Store.Logf("%d exercise(s) found, %d new", len(exercises), len(pending))

	if len(pending) == 0 {
		s.markSynced()

		return nil
	}

	garminToken, err := s.Garmin(ctx, false)
	if err != nil {
		s.Store.Logf("  Garmin: %v", err)

		return fmt.Errorf("garmin token: %w", err)
	}

	uploaded := s.uploadAll(ctx, pending, googleToken, garminToken)
	s.markSynced()
	s.Store.Logf("done: %d of %d pending uploaded", uploaded, len(pending))

	return nil
}

// Pending drops exercises already recorded in the ledger.
func Pending(exercises []google.Exercise, ledger map[string]string) []google.Exercise {
	var pending []google.Exercise

	for _, exercise := range exercises {
		if _, seen := ledger[exercise.ID]; !seen {
			pending = append(pending, exercise)
		}
	}

	return pending
}

func (s *Sync) uploadAll(ctx context.Context, pending []google.Exercise, googleToken, garminToken state.Token) int {
	uploaded := 0

	for _, exercise := range pending {
		s.Store.Logf("%s · %s", exercise.Label, exercise.ID)

		if s.uploadOne(ctx, exercise, &googleToken, &garminToken) {
			uploaded++
		}
	}

	return uploaded
}

func (s *Sync) uploadOne(ctx context.Context, exercise google.Exercise, googleToken, garminToken *state.Token) bool {
	tcx, err := s.Health.ExportTCX(ctx, googleToken.AccessToken, exercise.Name)
	if err != nil {
		// One bad export must not starve every later workout.
		s.Store.Logf("  export failed: %v", err)

		return false
	}

	status, err := s.Uploader.Upload(ctx, garminToken.AccessToken, tcx, exercise.ID)
	if err != nil {
		s.Store.Logf("  upload failed: %v", err)

		return false
	}

	if strings.Contains(status, "401") {
		renewed, renewErr := s.Garmin(ctx, true)
		if renewErr != nil {
			s.Store.Logf("  Garmin sign-in rejected: %v", renewErr)

			return false
		}

		*garminToken = renewed
		status, err = s.Uploader.Upload(ctx, garminToken.AccessToken, tcx, exercise.ID)
		if err != nil {
			s.Store.Logf("  upload failed: %v", err)

			return false
		}
	}

	s.Store.Logf("  %s", status)

	if status != garmin.StatusUploaded && status != garmin.StatusAlready {
		return false
	}

	s.record(exercise.ID)

	return true
}

func (s *Sync) record(exerciseID string) {
	_ = s.Store.Update(func(data *state.Data) error {
		data.Uploaded[exerciseID] = time.Now().Format(time.RFC3339)

		return nil
	})
}

func (s *Sync) markSynced() {
	_ = s.Store.Update(func(data *state.Data) error {
		data.LastSync = time.Now()

		return nil
	})
}

func (s *Sync) window() time.Duration {
	if s.Window <= 0 {
		return DefaultWindow
	}

	return s.Window
}
