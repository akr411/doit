package retry

import (
	"context"
	"errors"
	"math"
	"time"
)

type Config struct {
	MaxAttempts int
	InitialWait time.Duration
	MaxWait     time.Duration
	Multiplier  float64
}

var DefaultConfig = Config{
	MaxAttempts: 3,
	InitialWait: 1 * time.Second,
	MaxWait:     30 * time.Second,
	Multiplier:  2.0,
}

func Do(ctx context.Context, cfg Config, fn func() error) error {
	var lastErr error

	for attempt := 0; attempt < cfg.MaxAttempts; attempt++ {
		if attempt > 0 {
			wait := time.Duration(float64(cfg.InitialWait) * math.Pow(cfg.Multiplier, float64(attempt-1)))
			if wait > cfg.MaxWait {
				wait = cfg.MaxWait
			}

			select {
			case <-time.After(wait):
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		err := fn()
		if err == nil {
			return nil
		}

		lastErr = err

		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
	}

	return lastErr
}
