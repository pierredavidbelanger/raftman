package utils

import (
	"fmt"
	"regexp"
	"strconv"
	"time"
)

type Retention time.Duration

func (r Retention) String() string {
	if r < 0 {
		return "Infinite"
	}
	return time.Duration(r).String()
}

const INF Retention = Retention(-1)

var retentionRE *regexp.Regexp = regexp.MustCompile(`^(INF)$|^(?:(\d+)(w))?(?:(\d+)(d))?(?:(\d+)(h))?(?:(\d+)(m))?$`)

func ParseRetention(s string) (Retention, error) {
	if !retentionRE.MatchString(s) {
		return Retention(0), fmt.Errorf("invalid (INF|wdhm) duration '%s'", s)
	}
	sm := retentionRE.FindStringSubmatch(s)
	if sm[1] == "INF" {
		return INF, nil
	}
	var t Retention
	if sm[3] == "w" {
		n, _ := strconv.Atoi(sm[2])
		t += Retention(n) * 7 * 24 * Retention(time.Hour)
	}
	if sm[5] == "d" {
		n, _ := strconv.Atoi(sm[4])
		t += Retention(n) * 24 * Retention(time.Hour)
	}
	if sm[7] == "h" {
		n, _ := strconv.Atoi(sm[6])
		t += Retention(n) * Retention(time.Hour)
	}
	if sm[9] == "m" {
		n, _ := strconv.Atoi(sm[8])
		t += Retention(n) * Retention(time.Minute)
	}
	return t, nil
}
