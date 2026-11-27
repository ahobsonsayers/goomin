package syncer

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ahobsonsayers/goomin/google"
	"github.com/ahobsonsayers/goomin/state"
)

type fakeHealth struct {
	exercises []google.Exercise
	exports   map[string]string
	exported  []string
}

func (f *fakeHealth) Exercises(context.Context, string, time.Time) ([]google.Exercise, error) {
	return f.exercises, nil
}

func (f *fakeHealth) ExportTCX(_ context.Context, _, name string) ([]byte, error) {
	f.exported = append(f.exported, name)

	tcx, ok := f.exports[name]
	if !ok {
		return nil, errors.New("google refused the export")
	}

	return []byte(tcx), nil
}

type fakeUploader struct {
	statuses []string
	calls    []string
}

func (f *fakeUploader) Upload(_ context.Context, _ string, _ []byte, exerciseID string) (string, error) {
	f.calls = append(f.calls, exerciseID)

	if len(f.statuses) == 0 {
		return "uploaded", nil
	}

	status := f.statuses[0]
	f.statuses = f.statuses[1:]

	return status, nil
}

func TestPending(t *testing.T) {
	t.Parallel()

	exercises := []google.Exercise{{ID: "1"}, {ID: "2"}, {ID: "3"}}

	require.Len(t, Pending(exercises, map[string]string{"2": "then"}), 2)
	require.Empty(t, Pending(exercises, map[string]string{"1": "", "2": "", "3": ""}))
}

func TestRunUploadsOnlyNewExercises(t *testing.T) {
	t.Parallel()

	run, store := newRun(t)
	health := &fakeHealth{
		exercises: []google.Exercise{
			{ID: "1", Name: "exercise/1", Label: "run"},
			{ID: "2", Name: "exercise/2", Label: "ride"},
		},
		exports: map[string]string{"exercise/1": "<tcx/>", "exercise/2": "<tcx/>"},
	}
	uploader := &fakeUploader{}
	run.Health = health
	run.Uploader = uploader

	require.NoError(t, store.Update(func(data *state.Data) error {
		data.Uploaded["1"] = "before"

		return nil
	}))

	require.NoError(t, run.Run(context.Background()))

	require.Equal(t, []string{"2"}, uploader.calls)
	require.Contains(t, store.View().Uploaded, "2")
	require.Contains(t, logText(store), "done: 1 of 1 pending uploaded")
}

func TestRunKeepsGoingAfterAFailedExport(t *testing.T) {
	t.Parallel()

	run, store := newRun(t)
	uploader := &fakeUploader{}
	run.Health = &fakeHealth{
		exercises: []google.Exercise{{ID: "1", Name: "exercise/1"}, {ID: "2", Name: "exercise/2"}},
		exports:   map[string]string{"exercise/2": "<tcx/>"},
	}
	run.Uploader = uploader

	require.NoError(t, run.Run(context.Background()))

	require.Equal(t, []string{"2"}, uploader.calls)
	require.Contains(t, logText(store), "export failed")
}

func TestRunRenewsGarminOn401(t *testing.T) {
	t.Parallel()

	run, store := newRun(t)
	run.Health = &fakeHealth{
		exercises: []google.Exercise{{ID: "1", Name: "exercise/1"}},
		exports:   map[string]string{"exercise/1": "<tcx/>"},
	}
	run.Uploader = &fakeUploader{statuses: []string{"upload failed (401)", "uploaded"}}

	var forced []bool
	run.Garmin = func(_ context.Context, force bool) (state.Token, error) {
		forced = append(forced, force)

		return state.Token{AccessToken: "garmin"}, nil
	}

	require.NoError(t, run.Run(context.Background()))

	require.Equal(t, []bool{false, true}, forced)
	require.Contains(t, store.View().Uploaded, "1")
	require.Contains(t, logText(store), "done: 1 of 1 pending uploaded")
}

func TestRunWithNothingNew(t *testing.T) {
	t.Parallel()

	run, store := newRun(t)
	uploader := &fakeUploader{}
	run.Health = &fakeHealth{exercises: []google.Exercise{{ID: "1", Name: "exercise/1"}}}
	run.Uploader = uploader

	require.NoError(t, store.Update(func(data *state.Data) error {
		data.Uploaded["1"] = "before"

		return nil
	}))

	require.NoError(t, run.Run(context.Background()))

	require.Empty(t, uploader.calls)
	require.False(t, store.View().LastSync.IsZero())
}

func newRun(t *testing.T) (*Sync, *state.Store) {
	t.Helper()

	store, err := state.Open(filepath.Join(t.TempDir(), "state.json"))
	require.NoError(t, err)

	run := &Sync{
		Store:  store,
		Window: time.Hour,
		Google: func(context.Context, bool) (state.Token, error) {
			return state.Token{AccessToken: "google"}, nil
		},
		Garmin: func(context.Context, bool) (state.Token, error) {
			return state.Token{AccessToken: "garmin"}, nil
		},
	}

	return run, store
}

func logText(store *state.Store) string {
	return strings.Join(store.View().Log, "\n")
}
