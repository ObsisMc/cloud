package gateway

import (
	"context"
	"errors"
	"time"

	"go.uber.org/zap"
)

// RunCleanup deletes expired attempts and sessions past their audit window in bounded batches until
// ctx is canceled. It is owned by the process lifecycle: the caller cancels it on shutdown and waits
// for it to return. Failures are logged and retried at the next tick; they never stop the loop.
func RunCleanup(ctx context.Context, store *Store, interval, retention time.Duration, batch int, log *zap.Logger) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		// Keep deleting while whole batches are removed, so a backlog drains without one unbounded DELETE.
		for {
			n, e := store.Cleanup(ctx, retention, batch)
			if e != nil {
				if !errors.Is(e, context.Canceled) {
					log.Warn("gateway cleanup failed", zap.Error(e))
				}
				break
			}
			if n > 0 {
				log.Info("gateway cleanup", zap.Int64("deleted", n))
			}
			if n < int64(batch) {
				break
			}
		}
	}
}
