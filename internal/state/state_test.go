package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestStoreRoundTrip(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "nested", "state.json")

	store, err := Open(path)
	require.NoError(t, err)

	require.NoError(t, store.Update(func(data *Data) error {
		data.Google = Token{AccessToken: "a", RefreshToken: "r", ExpiresAt: time.Now().Add(time.Hour)}
		data.Uploaded["1"] = "then"

		return nil
	}))

	info, err := os.Stat(path)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	reopened, err := Open(path)
	require.NoError(t, err)
	require.Equal(t, "a", reopened.View().Google.AccessToken)
	require.Equal(t, "then", reopened.View().Uploaded["1"])
}

func TestOpenMissingFile(t *testing.T) {
	t.Parallel()

	store, err := Open(filepath.Join(t.TempDir(), "state.json"))
	require.NoError(t, err)
	require.False(t, store.View().Google.Connected())
	require.NotNil(t, store.View().Uploaded)
}

func TestTokenExpiry(t *testing.T) {
	t.Parallel()

	require.True(t, Token{}.Expired())
	require.True(t, Token{ExpiresAt: time.Now().Add(30 * time.Second)}.Expired())
	require.False(t, Token{ExpiresAt: time.Now().Add(time.Hour)}.Expired())
	require.False(t, Token{}.Connected())
}

func TestLogKeepsNewestLines(t *testing.T) {
	t.Parallel()

	store, err := Open(filepath.Join(t.TempDir(), "state.json"))
	require.NoError(t, err)

	for i := range logLimit + 5 {
		store.Logf("line %d", i)
	}

	lines := store.View().Log
	require.Len(t, lines, logLimit)
	require.Contains(t, lines[len(lines)-1], "line 204")
}
