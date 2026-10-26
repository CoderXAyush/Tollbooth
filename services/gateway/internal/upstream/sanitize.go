package upstream

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

var (
	// ExposedToolNameRegex matches valid exposed tool names: 1 to 64 chars, alphanumeric, underscores, and hyphens.
	ExposedToolNameRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

	// URLRegex matches http:// and https:// URLs.
	urlRegex = regexp.MustCompile(`https?://[^\s/$.?#].[^\s]*`)

	// IPWithPortRegex matches IPv4 addresses optionally followed by a port.
	ipWithPortRegex = regexp.MustCompile(`\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}(:\d+)?\b`)

	// HostPortRegex matches host:port pairs commonly found in network errors (e.g. "woo-mcp:8081", "localhost:8080").
	hostPortRegex = regexp.MustCompile(`\b[a-zA-Z0-9][-a-zA-Z0-9.]*:\d+\b`)

	ErrInvalidToolName = errors.New("tool name must match ^[a-zA-Z0-9_-]{1,64}$")
	ErrNoDelimiter     = errors.New("exposed tool name must contain '__' delimiter")
)

const (
	// MaxDescriptionLength caps description length for untrusted upstream tools to 2000 chars.
	MaxDescriptionLength = 2000
)

// BuildExposedName constructs the namespaced tool name: <slug>__<tool>.
// If the combined name violates ^[a-zA-Z0-9_-]{1,64}$, an error is returned.
func BuildExposedName(slug, tool string) (string, error) {
	slug = strings.TrimSpace(slug)
	tool = strings.TrimSpace(tool)
	if slug == "" || tool == "" {
		return "", ErrInvalidToolName
	}
	exposed := slug + "__" + tool
	if err := ValidateExposedName(exposed); err != nil {
		return "", err
	}
	return exposed, nil
}

// SplitExposedName splits an exposed tool name on the FIRST '__' delimiter.
func SplitExposedName(name string) (slug string, tool string, err error) {
	name = strings.TrimSpace(name)
	idx := strings.Index(name, "__")
	if idx <= 0 || idx+2 >= len(name) {
		return "", "", ErrNoDelimiter
	}
	slug = name[:idx]
	tool = name[idx+2:]
	if err := ValidateExposedName(name); err != nil {
		return "", "", err
	}
	return slug, tool, nil
}

// ValidateExposedName verifies that the exposed name complies with ^[a-zA-Z0-9_-]{1,64}$.
func ValidateExposedName(name string) error {
	if len(name) == 0 || len(name) > 64 || !ExposedToolNameRegex.MatchString(name) {
		return ErrInvalidToolName
	}
	return nil
}

// CleanDescription enforces a 2000-character cap and strips control characters,
// preserving standard whitespace characters (tab, newline, carriage return).
// It never injects extra text to avoid prompt injection vulnerabilities.
func CleanDescription(raw string) string {
	if len(raw) > MaxDescriptionLength {
		raw = raw[:MaxDescriptionLength]
	}

	var b strings.Builder
	b.Grow(len(raw))
	for _, r := range raw {
		if r < 32 {
			if r == '\t' || r == '\n' || r == '\r' {
				b.WriteRune(r)
			}
			continue
		}
		if r == 127 || unicode.Is(unicode.Cc, r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// SanitiseError strips hostnames, IPs, ports, URLs, and credential patterns from error messages.
func SanitiseError(err error, slug string) string {
	if err == nil {
		return ""
	}
	msg := err.Error()

	// Check for common timeout indicators
	lower := strings.ToLower(msg)
	if strings.Contains(lower, "deadline exceeded") || strings.Contains(lower, "timed out") || strings.Contains(lower, "timeout") {
		return fmt.Sprintf("server '%s' request timed out", slug)
	}

	if strings.Contains(lower, "connection refused") || strings.Contains(lower, "no such host") || strings.Contains(lower, "dial tcp") {
		return fmt.Sprintf("server '%s' is unreachable", slug)
	}

	// Remove URLs
	msg = urlRegex.ReplaceAllString(msg, "[redacted-url]")

	// Remove IPs with optional ports
	msg = ipWithPortRegex.ReplaceAllString(msg, "[redacted-ip]")

	// Remove Host:port patterns
	msg = hostPortRegex.ReplaceAllString(msg, "[redacted-host]")

	// Ensure no raw bearer tokens leak
	if strings.Contains(lower, "bearer") || strings.Contains(lower, "token") {
		msg = "[redacted authorization error]"
	}

	msg = strings.TrimSpace(msg)
	if msg == "" {
		return fmt.Sprintf("server '%s' encountered an error", slug)
	}
	return msg
}
