package store

import (
	"fmt"
	"strconv"
	"time"
)

// Retention is how long entries are kept. Infinite keeps them forever.
type Retention time.Duration

const Infinite Retention = -1

func (r Retention) String() string {
	if r < 0 {
		return "Infinite"
	}
	return time.Duration(r).String()
}

// ParseRetention accepts "INF" or a sequence of <n>w, <n>d, <n>h, <n>m, in
// that order and each at most once, such as "1w2d" or "12h". The empty string
// is a zero retention.
func ParseRetention(s string) (Retention, error) {
	if s == "INF" {
		return Infinite, nil
	}
	units := []struct {
		suffix byte
		unit   time.Duration
	}{
		{'w', 7 * 24 * time.Hour},
		{'d', 24 * time.Hour},
		{'h', time.Hour},
		{'m', time.Minute},
	}
	var total time.Duration
	rest := s
	for _, u := range units {
		i := 0
		for i < len(rest) && rest[i] >= '0' && rest[i] <= '9' {
			i++
		}
		if i == 0 || i == len(rest) || rest[i] != u.suffix {
			continue
		}
		n, err := strconv.Atoi(rest[:i])
		if err != nil {
			break
		}
		total += time.Duration(n) * u.unit
		rest = rest[i+1:]
	}
	if rest != "" {
		return 0, fmt.Errorf("invalid (INF|wdhm) duration '%s'", s)
	}
	return Retention(total), nil
}
