package upstream

import (
	"errors"
	"strings"
	"testing"
)

func TestBuildAndSplitExposedName(t *testing.T) {
	tests := []struct {
		slug        string
		tool        string
		expected    string
		expectErr   bool
	}{
		{"woo", "list_products", "woo__list_products", false},
		{"woo-b", "get_order", "woo-b__get_order", false},
		{"s1", "a", "s1__a", false},
		{"", "list_products", "", true},
		{"woo", "", "", true},
		{"woo", "tool_with_special!chars", "", true},
		{"woo", strings.Repeat("a", 70), "", true}, // Exceeds 64 chars
	}

	for _, tc := range tests {
		exposed, err := BuildExposedName(tc.slug, tc.tool)
		if tc.expectErr {
			if err == nil {
				t.Errorf("BuildExposedName(%q, %q) expected error, got nil", tc.slug, tc.tool)
			}
			continue
		}
		if err != nil {
			t.Errorf("BuildExposedName(%q, %q) unexpected error: %v", tc.slug, tc.tool, err)
		}
		if exposed != tc.expected {
			t.Errorf("BuildExposedName(%q, %q) = %q, expected %q", tc.slug, tc.tool, exposed, tc.expected)
		}

		// Verify round-trip split
		slug, originTool, err := SplitExposedName(exposed)
		if err != nil {
			t.Errorf("SplitExposedName(%q) unexpected error: %v", exposed, err)
		}
		if slug != tc.slug || originTool != tc.tool {
			t.Errorf("SplitExposedName(%q) = (%q, %q), expected (%q, %q)", exposed, slug, originTool, tc.slug, tc.tool)
		}
	}
}

func TestSplitExposedName_FirstDelimiter(t *testing.T) {
	// Must split on FIRST '__' only
	name := "woo__some__nested__tool"
	slug, tool, err := SplitExposedName(name)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if slug != "woo" {
		t.Errorf("expected slug 'woo', got %q", slug)
	}
	if tool != "some__nested__tool" {
		t.Errorf("expected tool 'some__nested__tool', got %q", tool)
	}
}

func TestCleanDescription(t *testing.T) {
	// Test cap at 2000 chars
	longDesc := strings.Repeat("x", 2500)
	cleaned := CleanDescription(longDesc)
	if len(cleaned) != MaxDescriptionLength {
		t.Errorf("expected length %d, got %d", MaxDescriptionLength, len(cleaned))
	}

	// Test stripping control characters, but preserving \n, \t, \r
	input := "Hello\x00\x07World\t\nGood\rMorning\x1b[31m!"
	cleaned = CleanDescription(input)
	expected := "HelloWorld\t\nGood\rMorning[31m!"
	if cleaned != expected {
		t.Errorf("CleanDescription(%q) = %q, expected %q", input, cleaned, expected)
	}
}

func TestSanitiseError(t *testing.T) {
	tests := []struct {
		err      error
		slug     string
		assertFn func(t *testing.T, res string)
	}{
		{
			err:  errors.New("dial tcp 10.96.12.34:8081: connect: connection refused"),
			slug: "woo",
			assertFn: func(t *testing.T, res string) {
				if strings.Contains(res, "10.96.12.34") || strings.Contains(res, "8081") {
					t.Errorf("error leaked IP or port: %q", res)
				}
				if !strings.Contains(res, "server 'woo' is unreachable") {
					t.Errorf("expected unreachable message, got %q", res)
				}
			},
		},
		{
			err:  errors.New("Post \"http://woo-mcp.tollbooth.svc:8081/mcp\": context deadline exceeded"),
			slug: "woo-b",
			assertFn: func(t *testing.T, res string) {
				if strings.Contains(res, "woo-mcp") || strings.Contains(res, "8081") || strings.Contains(res, "http") {
					t.Errorf("error leaked host, port, or url: %q", res)
				}
				if !strings.Contains(res, "request timed out") {
					t.Errorf("expected timeout message, got %q", res)
				}
			},
		},
		{
			err:  errors.New("Authorization header invalid: Bearer secret-token-xyz123"),
			slug: "woo",
			assertFn: func(t *testing.T, res string) {
				if strings.Contains(res, "secret-token") {
					t.Errorf("error leaked secret token: %q", res)
				}
			},
		},
	}

	for _, tc := range tests {
		res := SanitiseError(tc.err, tc.slug)
		tc.assertFn(t, res)
	}
}
