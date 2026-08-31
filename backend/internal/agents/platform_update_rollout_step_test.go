package agents

import (
	"errors"
	"fmt"
	"testing"
)

func TestShouldNormalizePlatformUpdateRolloutAdmissionError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "complete", err: ErrPlatformUpdateRolloutComplete, want: true},
		{name: "wrapped complete", err: fmt.Errorf("wrapped: %w", ErrPlatformUpdateRolloutComplete), want: true},
		{name: "not mutation runnable", err: ErrPlatformUpdateRolloutNotMutationRunnable, want: true},
		{name: "wrapped not mutation runnable", err: fmt.Errorf("wrapped: %w", ErrPlatformUpdateRolloutNotMutationRunnable), want: true},
		{name: "durable admission failure", err: ErrPlatformUpdateRolloutAdmissionFailed, want: true},
		{name: "wrapped durable admission failure", err: fmt.Errorf("wrapped: %w", ErrPlatformUpdateRolloutAdmissionFailed), want: true},
		{name: "infrastructure error", err: errors.New("database unavailable"), want: false},
		{name: "nil", err: nil, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldNormalizePlatformUpdateRolloutAdmissionError(tt.err); got != tt.want {
				t.Fatalf("shouldNormalizePlatformUpdateRolloutAdmissionError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
