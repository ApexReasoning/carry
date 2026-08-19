package login

import "testing"

func TestParseServerURLRequiresHTTPSRoot(t *testing.T) {
	t.Parallel()

	for _, valid := range []string{
		"https://carry.example.com",
		"https://127.0.0.1:8443/",
	} {
		if _, err := parseServerURL(valid); err != nil {
			t.Errorf("valid URL %q rejected: %v", valid, err)
		}
	}
	for _, invalid := range []string{
		"http://carry.example.com",
		"carry.example.com",
		"https://user@carry.example.com",
		"https://carry.example.com/v1",
		"https://carry.example.com?token=secret",
		"https://carry.example.com#fragment",
	} {
		if _, err := parseServerURL(invalid); err == nil {
			t.Errorf("invalid URL %q accepted", invalid)
		}
	}
}
