package syncer

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/ahobsonsayers/goomin/internal/garmin"
	"github.com/ahobsonsayers/goomin/internal/google"
	"github.com/ahobsonsayers/goomin/internal/state"
)

// DefaultWindow is wider than the daily interval: a weekly Google re-consent must not drop a day.
const DefaultWindow = 90 * 24 * time.Hour

// Steps reads per-day totals from the health provider.
type Steps interface {
	DailySteps(ctx context.Context, accessToken string, since time.Time) ([]google.StepDay, error)
}

// StepsImport writes one completed day of steps to Garmin.
type StepsImport interface {
	ImportSteps(ctx context.Context, accessToken, date string, steps int) (string, error)
}

// Tokens returns a usable access token, renewing it when force is set.
type Tokens func(ctx context.Context, force bool) (state.Token, error)

// Sync imports each completed day of Google Health steps into Garmin.
type Sync struct {
	Store       *state.Store
	Steps       Steps
	StepsImport StepsImport
	Google      Tokens
	Garmin      Tokens
	Window      time.Duration

	running atomic.Bool
}

// Run imports every completed day of steps that is not in the ledger yet.
func (s *Sync) Run(ctx context.Context) error {
	if !s.running.CompareAndSwap(false, true) {
		s.Store.Logf("a sync is already running")

		return nil
	}
	defer s.running.Store(false)

	since := time.Now().Add(-s.window())
	s.Store.Logf("Google Health steps, since %s", since.Format("2006-01-02"))

	googleToken, err := s.Google(ctx, false)
	if err != nil {
		s.Store.Logf("Google: %v", err)

		return fmt.Errorf("google token: %w", err)
	}

	garminToken, err := s.Garmin(ctx, false)
	if err != nil {
		s.Store.Logf("Garmin: %v", err)

		return fmt.Errorf("garmin token: %w", err)
	}

	s.importSteps(ctx, googleToken, garminToken, time.Now())
	s.markSynced()

	return nil
}

// importSteps writes one Other activity per completed day of steps. The
// ongoing day is skipped: its total keeps growing, and re-importing would
// duplicate.
func (s *Sync) importSteps(ctx context.Context, googleToken, garminToken state.Token, now time.Time) {
	stepsDays, err := s.Steps.DailySteps(ctx, googleToken.AccessToken, now.Add(-s.window()))
	if err != nil {
		s.Store.Logf("steps: %v", err)

		return
	}

	imported := 0

	for _, day := range stepsDays {
		if day.Date >= now.Format(time.DateOnly) {
			continue
		}

		ledgerKey := "steps-" + day.Date
		if _, seen := s.Store.View().Uploaded[ledgerKey]; seen {
			continue
		}

		s.Store.Logf("%s · %d steps", day.Date, day.Steps)

		status, err := s.StepsImport.ImportSteps(ctx, garminToken.AccessToken, day.Date, day.Steps)
		if err != nil {
			s.Store.Logf("  import failed: %v", err)

			continue
		}

		s.Store.Logf("  %s", status)

		if status != garmin.StatusUploaded && status != garmin.StatusAlready {
			continue
		}

		s.record(ledgerKey)
		imported++
	}

	if imported > 0 {
		s.Store.Logf("done: %d day(s) of steps", imported)
	}
}

func (s *Sync) record(dayKey string) {
	_ = s.Store.Update(func(data *state.Data) error {
		data.Uploaded[dayKey] = time.Now().Format(time.RFC3339)

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
