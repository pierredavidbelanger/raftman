package utils

import (
	"net/url"
	"strconv"
	"time"
	"gopkg.in/mcuadros/go-syslog.v2/format"
	"strings"
	"gopkg.in/mcuadros/go-syslog.v2"
	"fmt"
)

func GetIntQueryParam(u *url.URL, name string, defaultValue int) (int, error) {
	s := u.Query().Get(name)
	if s == "" {
		return defaultValue, nil
	}
	return strconv.Atoi(s)
}

func GetDurationQueryParam(u *url.URL, name string, defaultValue time.Duration) (time.Duration, error) {
	s := u.Query().Get(name)
	if s == "" {
		return defaultValue, nil
	}
	return time.ParseDuration(s)
}

func GetRetentionQueryParam(u *url.URL, name string, defaultValue Retention) (Retention, error) {
	s := u.Query().Get(name)
	if s == "" {
		return defaultValue, nil
	}
	return ParseRetention(s)
}

func GetSyslogFormatQueryParam(u *url.URL, name string, defaultValue format.Format) (format.Format, error) {
	s := u.Query().Get(name)
	if s == "" {
		return defaultValue, nil
	}
	// TODO: must support them in syslog.toLogEntry
	switch strings.ToUpper(s) {
	case "RFC3164":
		return syslog.RFC3164, nil
	case "RFC5424":
		return syslog.RFC5424, nil
	//case "RFC6587":
	//	return syslog.RFC6587, nil
	//case "AUTOMATIC":
	//	return syslog.Automatic, nil
	}
	return nil, fmt.Errorf("Invalid syslog format %s", s)
}
