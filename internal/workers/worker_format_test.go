package workers

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestFormatRemaining(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   time.Duration
		want string
	}{
		{name: "30m", in: 30 * time.Minute, want: "час"},
		{name: "59m", in: 59 * time.Minute, want: "час"},
		{name: "1h", in: time.Hour, want: "~1 ч"},
		{name: "2h59m", in: 2*time.Hour + 59*time.Minute, want: "~3 ч"},
		{name: "23h31m", in: 23*time.Hour + 31*time.Minute, want: "~24 ч"},
		{name: "24h", in: 24 * time.Hour, want: "~24 ч"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, formatRemaining(tc.in))
		})
	}
}
