package worker

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestEveryRunsUntilCancelled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	var runs atomic.Int32

	done := make(chan struct{})
	go func() {
		defer close(done)
		Every(ctx, time.Millisecond, func(context.Context) { runs.Add(1) })
	}()

	require.Eventually(t, func() bool { return runs.Load() >= 3 }, time.Second, time.Millisecond)

	cancel()
	<-done
	require.GreaterOrEqual(t, runs.Load(), int32(3))
}
