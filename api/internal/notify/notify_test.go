package notify

import (
	"testing"
	"time"
)

func TestReminderDate(t *testing.T) {
	d := func(s string) time.Time {
		t, _ := time.ParseInLocation("2006-01-02", s, jst)
		return t
	}
	tests := []struct {
		deadline string
		days     int
		want     string
	}{
		{"2026-10-15", 3, "2026-10-12"}, // 木曜の締切の3日前は月曜
		{"2026-10-15", 1, "2026-10-14"},
		{"2026-10-19", 1, "2026-10-16"}, // 月曜の締切の前日（日曜）は直前の金曜
		{"2026-10-20", 3, "2026-10-16"}, // 火曜の締切の3日前（土曜）は直前の金曜
	}
	for _, tt := range tests {
		if got := reminderDate(d(tt.deadline), tt.days).Format("2006-01-02"); got != tt.want {
			t.Errorf("reminderDate(%s, %d) = %s, want %s", tt.deadline, tt.days, got, tt.want)
		}
	}
}

func TestMonthsLabel(t *testing.T) {
	if got := monthsLabel([]string{"2026-04", "2026-05"}); got != "4月・5月" {
		t.Errorf("monthsLabel = %q", got)
	}
}
