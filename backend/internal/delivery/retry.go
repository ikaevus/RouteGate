package delivery

import "time"

type RetryPolicy struct {
	BaseDelay time.Duration
	MaxDelay  time.Duration
}

func DefaultRetryPolicy() RetryPolicy {
	return RetryPolicy{BaseDelay: 5 * time.Second, MaxDelay: 5 * time.Minute}
}

func (p RetryPolicy) Delay(attempt int) time.Duration {
	base := p.BaseDelay
	if base <= 0 {
		base = 5 * time.Second
	}
	maximum := p.MaxDelay
	if maximum <= 0 {
		maximum = 5 * time.Minute
	}
	if attempt < 1 {
		attempt = 1
	}
	delay := base
	for current := 1; current < attempt && delay < maximum; current++ {
		if delay > maximum/2 {
			delay = maximum
			break
		}
		delay *= 2
	}
	if delay > maximum {
		return maximum
	}
	return delay
}
