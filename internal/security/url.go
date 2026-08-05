package security

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
)

var projectRefPattern = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

func ValidateHostedProjectURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, errors.New("project URL is invalid")
	}
	if u.Scheme != "https" || u.Opaque != "" {
		return nil, errors.New("project URL must use HTTPS")
	}
	if u.User != nil || u.Port() != "" || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("project URL must be a bare hosted Supabase origin")
	}
	if u.Path != "" && u.Path != "/" {
		return nil, errors.New("project URL must not contain a path")
	}
	host := u.Hostname()
	if host == "" || host != strings.ToLower(host) || strings.HasSuffix(host, ".") {
		return nil, errors.New("project URL hostname is invalid")
	}
	labels := strings.Split(host, ".")
	if len(labels) != 3 || labels[1] != "supabase" || labels[2] != "co" || !projectRefPattern.MatchString(labels[0]) {
		return nil, errors.New("project URL must match https://<project-ref>.supabase.co")
	}
	return &url.URL{Scheme: "https", Host: host}, nil
}
