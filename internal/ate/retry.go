package ate

import (
	"context"
	"math"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const DefaultResumeAttempts = 30

type Backoff struct {
	Initial time.Duration
	Max     time.Duration
	Factor  float64
}

func DefaultResumeBackoff() Backoff {
	return Backoff{Initial: 200 * time.Millisecond, Max: 2 * time.Second, Factor: 2}
}

func IsResumeParkingHTTP(statusCode int, body []byte) bool {
	if statusCode != 503 {
		return false
	}
	return strings.Contains(strings.ToLower(string(body)), "still resuming")
}

func IsRetryableResume(err error) bool {
	switch status.Code(err) {
	case codes.ResourceExhausted, codes.Aborted:
		return true
	default:
		return false
	}
}

func (b Backoff) Wait(ctx context.Context, attempt int) error {
	if attempt < 0 {
		attempt = 0
	}
	d := b.Initial
	if b.Factor > 1 && attempt > 0 {
		mult := math.Pow(b.Factor, float64(attempt))
		d = time.Duration(float64(b.Initial) * mult)
	}
	if b.Max > 0 && d > b.Max {
		d = b.Max
	}
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
