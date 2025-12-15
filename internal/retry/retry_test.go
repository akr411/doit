package retry

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRetryDo(t *testing.T) {
	tests := []struct {
		name        string
		attempts    int
		failUntil   int
		expectErr   bool
		expectCalls int
	}{
		{"success first try", 3, 0, false, 1},
		{"success second try", 3, 1, false, 2},
		{"success third try", 3, 2, false, 3},
		{"fail all attempts", 3, 5, true, 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Config{
				MaxAttempts: tt.attempts,
				InitialWait: time.Millisecond,
				MaxWait:     10 * time.Millisecond,
				Multiplier:  2.0,
			}
			calls := 0

			fn := func() error {
				calls++
				if calls <= tt.failUntil {
					return errors.New("temp error")
				}
				return nil
			}

			ctx := context.Background()
			err := Do(ctx, cfg, fn)

			if (err != nil) != tt.expectErr {
				t.Errorf("Do() error = %v, wantErr %v", err, tt.expectErr)
			}

			if calls != tt.expectCalls {
				t.Errorf("Expected %d calls, got %d", tt.expectCalls, calls)
			}
		})
	}
}

func TestRetryContextCanceled(t *testing.T) {
	cfg := Config{
		MaxAttempts: 10,
		InitialWait: 100 * time.Millisecond,
		MaxWait:     time.Second,
		Multiplier:  2.0,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	calls := 0
	err := Do(ctx, cfg, func() error {
		calls++
		return errors.New("should not retry")
	})

	if err != context.Canceled {
		t.Errorf("Expected context.Canceled, got %v", err)
	}

	if calls > 1 {
		t.Errorf("Expected at most 1 call with canceled context, got %d", calls)
	}
}

func TestRetryExponentialBackoff(t *testing.T) {
	cfg := Config{
		MaxAttempts: 4,
		InitialWait: 10 * time.Millisecond,
		MaxWait:     100 * time.Millisecond,
		Multiplier:  2.0,
	}

	start := time.Now()
	calls := 0

	ctx := context.Background()
	Do(ctx, cfg, func() error {
		calls++
		return errors.New("keep failing")
	})

	elapsed := time.Since(start)

	expectedMin := 10 + 20 + 40
	if elapsed < time.Duration(expectedMin)*time.Millisecond {
		t.Errorf("Expected at least %dms for backoff, got %v", expectedMin, elapsed)
	}
}

func TestRetryMaxWait(t *testing.T) {
	cfg := Config{
		MaxAttempts: 5,
		InitialWait: 10 * time.Millisecond,
		MaxWait:     30 * time.Millisecond,
		Multiplier:  10.0,
	}

	start := time.Now()

	ctx := context.Background()
	Do(ctx, cfg, func() error {
		return errors.New("fail")
	})

	elapsed := time.Since(start)

	expectedMax := 30 + 30 + 30 + 30 + 50
	if elapsed > time.Duration(expectedMax)*time.Millisecond {
		t.Errorf("Backoff exceeded max wait cap, took %v", elapsed)
	}
}
