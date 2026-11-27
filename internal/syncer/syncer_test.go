package syncer

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ahobsonsayers/goomin/internal/google"
	"github.com/ahobsonsayers/goomin/internal/state"
)

type fakeSteps struct {
	days []google.StepDay
	err  error
}

func (f *fakeSteps) DailySteps(context.Context, string, time.Time) ([]google.StepDay, error) {
	return f.days, f.err
}

type fakeStepsImport struct {
	imported []google.StepDay
}

func (f *fakeStepsImport) ImportSteps(_ context.Context, _, date string, steps int) (string, error) {
	f.imported = append(f.imported, google.StepDay{Date: date, Steps: steps})

	return "uploaded", nil
}

func TestImportStepsOnlyImportsCompletedDays(t *testing.T) {
	t.Parallel()

	run, store := newRun(t)

	days := []google.StepDay{
		{Date: "2026-11-25", Steps: 100}, // new, imports
		{Date: "2026-11-26", Steps: 0},   // no steps, nothing to import
		{Date: "2026-11-26", Steps: 50},  // today, still growing
		{Date: "2026-11-24", Steps: 200}, // already ledgered
		{Date: "2026-11-27", Steps: 5},   // future
	}
	run.Steps = &fakeSteps{days: days}

	require.NoError(t, store.Update(func(data *state.Data) error {
		data.Uploaded["steps-2026-11-24"] = "before"

		return nil
	}))

	run.importSteps(
		context.Background(),
		state.Token{},
		state.Token{},
		time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC),
	)

	require.Len(t, run.StepsImport.(*fakeStepsImport).imported, 1)
	require.Equal(t, days[0], run.StepsImport.(*fakeStepsImport).imported[0])
	require.Contains(t, store.View().Uploaded, "steps-2026-11-25")

	log := logText(store)
	require.Contains(t, log, "2026-11-25 · 100 steps")
	require.Contains(t, log, "done: 1 day(s) of steps")
}

func TestImportStepsFailsSoft(t *testing.T) {
	t.Parallel()

	run, store := newRun(t)
	run.Steps = &fakeSteps{err: context.DeadlineExceeded}

	run.importSteps(context.Background(), state.Token{}, state.Token{}, time.Now())

	require.Empty(t, run.StepsImport.(*fakeStepsImport).imported)
	require.Contains(t, logText(store), "steps: context deadline exceeded")
}

func TestRunSkipsAlreadyImportedDays(t *testing.T) {
	t.Parallel()

	run, store := newRun(t)

	yesterday := time.Now().AddDate(0, 0, -1).Format(time.DateOnly)

	run.Steps = &fakeSteps{days: []google.StepDay{
		{Date: yesterday, Steps: 100},
		{Date: time.Now().Format(time.DateOnly), Steps: 40},
	}}

	require.NoError(t, store.Update(func(data *state.Data) error {
		data.Uploaded["steps-"+yesterday] = "before"

		return nil
	}))

	require.NoError(t, run.Run(context.Background()))

	require.Empty(t, run.StepsImport.(*fakeStepsImport).imported)
	require.False(t, store.View().LastSync.IsZero())
	require.NotContains(t, logText(store), "done:")
}

func newRun(t *testing.T) (*Sync, *state.Store) {
	t.Helper()

	store, err := state.Open(filepath.Join(t.TempDir(), "state.json"))
	require.NoError(t, err)

	return &Sync{
		Store:       store,
		Steps:       &fakeSteps{},
		StepsImport: &fakeStepsImport{}, //nolint:exhaustruct // Google and Garmin tokens are set per test.
		Google: func(context.Context, bool) (state.Token, error) {
			return state.Token{}, nil
		},
		Garmin: func(context.Context, bool) (state.Token, error) {
			return state.Token{}, nil
		},
	}, store
}

func logText(store *state.Store) string {
	return strings.Join(store.View().Log, "\n")
}
