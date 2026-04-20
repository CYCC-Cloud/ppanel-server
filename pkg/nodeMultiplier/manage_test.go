package nodeMultiplier

import (
	"testing"
	"time"
)

func TestNewNodeMultiplierManager(t *testing.T) {
	periods := []TimePeriod{
		{
			StartTime:  "23:00.000",
			EndTime:    "1:59.000",
			Multiplier: 1.2,
		},
		{
			StartTime:  "12:00.000",
			EndTime:    "13:59.000",
			Multiplier: 0.5,
		},
	}
	m := NewNodeMultiplierManager(periods)
	if len(m.Periods) != len(periods) {
		t.Fatalf("expected %d periods, got %d", len(periods), len(m.Periods))
	}

	tests := []struct {
		name string
		at   time.Time
		want float32
	}{
		{
			name: "midnight crossing period after midnight",
			at:   time.Date(0, 1, 1, 0, 10, 0, 0, time.UTC),
			want: 1.2,
		},
		{
			name: "midnight crossing period before midnight",
			at:   time.Date(0, 1, 1, 23, 10, 0, 0, time.UTC),
			want: 1.2,
		},
		{
			name: "daytime period returns configured multiplier",
			at:   time.Date(0, 1, 1, 12, 30, 0, 0, time.UTC),
			want: 0.5,
		},
		{
			name: "outside all periods falls back to default",
			at:   time.Date(0, 1, 1, 14, 0, 0, 0, time.UTC),
			want: 1,
		},
		{
			name: "start boundary is exclusive for midnight crossing period",
			at:   time.Date(0, 1, 1, 23, 0, 0, 0, time.UTC),
			want: 1,
		},
		{
			name: "end boundary is exclusive for midnight crossing period",
			at:   time.Date(0, 1, 1, 1, 59, 0, 0, time.UTC),
			want: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := m.GetMultiplier(tt.at); got != tt.want {
				t.Fatalf("GetMultiplier(%s) = %v, want %v", tt.at.Format("15:04"), got, tt.want)
			}
		})
	}
}
