// Package scheduler runs the periodic background jobs (the daily plan, the
// outbox) until its context is cancelled.
package scheduler

import (
	"context"
	"log/slog"
	"time"
)

// Job is one periodic unit of work. Run returns how many items it handled.
type Job struct {
	Name string
	Run  func(ctx context.Context) (int, error)
}

// Run runs every job once, then again on each tick, until ctx is done. A failing
// job is logged and does not stop the other jobs or the loop. Errors are logged
// without message bodies or personal data.
func Run(ctx context.Context, every time.Duration, log *slog.Logger, jobs ...Job) {
	if log == nil {
		log = slog.Default()
	}
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		for _, j := range jobs {
			if ctx.Err() != nil {
				return
			}
			n, err := j.Run(ctx)
			switch {
			case err != nil:
				log.Warn("job failed", "job", j.Name, "err", err)
			case n > 0:
				log.Info("job ran", "job", j.Name, "handled", n)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
