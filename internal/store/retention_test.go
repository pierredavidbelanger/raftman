package store

import (
	"testing"
	"time"
)

func TestParseRetention(t *testing.T) {
	const h = Retention(time.Hour)
	cases := []struct {
		in   string
		want Retention
	}{
		{"INF", Infinite},
		{"", 0},
		{"1w", 7 * 24 * h},
		{"2d", 48 * h},
		{"3h", 3 * h},
		{"4m", 4 * Retention(time.Minute)},
		{"1w2d3h4m", 7*24*h + 48*h + 3*h + 4*Retention(time.Minute)},
		{"1w4m", 7*24*h + 4*Retention(time.Minute)},
		{"0d", 0},
	}
	for _, c := range cases {
		got, err := ParseRetention(c.in)
		if err != nil || got != c.want {
			t.Errorf("ParseRetention(%q) = %v, %v; want %v", c.in, got, err, c.want)
		}
	}
	for _, in := range []string{"inf", "1x", "1s", "1h1w", "1h 2m", "1.5h", "-1h", "w", "INF1h", "1", "1h1h", "99999999999999999999h"} {
		if got, err := ParseRetention(in); err == nil {
			t.Errorf("ParseRetention(%q) = %v, want error", in, got)
		}
	}
}

func TestRetentionString(t *testing.T) {
	if s := Infinite.String(); s != "Infinite" {
		t.Errorf("Infinite.String() = %q", s)
	}
	if s := Retention(90 * time.Minute).String(); s != "1h30m0s" {
		t.Errorf("String() = %q", s)
	}
}
