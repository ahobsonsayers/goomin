package worker

import (
	"context"
	"time"
)

// Every calls fn immediately, then again every interval.
func Every(ctx context.Context, interval time.Duration, fn func(context.Context)) {
	fn(ctx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			fn(ctx)
		}
	}
}
