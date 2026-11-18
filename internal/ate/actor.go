package ate

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

var dnsLabel = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

// ActorName builds a DNS-1123 actor name: {owner}-{repo}-{agent}.
func ActorName(owner, repo, agent string) (string, error) {
	owner = sanitizeDNSLabel(owner)
	repo = sanitizeDNSLabel(repo)
	agent = sanitizeDNSLabel(agent)
	if owner == "" || repo == "" || agent == "" {
		return "", fmt.Errorf("actor name requires owner, repo, and agent")
	}
	name := owner + "-" + repo + "-" + agent
	if len(name) > 63 {
		name = name[:63]
		name = strings.TrimRight(name, "-")
	}
	if !dnsLabel.MatchString(name) {
		return "", fmt.Errorf("invalid actor name %q", name)
	}
	return name, nil
}

func sanitizeDNSLabel(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	for _, r := range s {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		case r == '-' || r == '_' || r == '.':
			b.WriteByte('-')
		}
	}
	out := b.String()
	out = regexp.MustCompile(`-+`).ReplaceAllString(out, "-")
	out = strings.Trim(out, "-")
	return out
}

// TargetHeader returns the ate-target-actor routing header value.
func TargetHeader(atespace, actor string) string {
	return strings.TrimSpace(atespace) + "/" + strings.TrimSpace(actor)
}
