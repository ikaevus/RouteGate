package publicurl

import (
	"errors"
	"testing"
)

func TestNormalizeAcceptsBareHTTPSOrigin(t *testing.T) {
	got, err := Normalize("https://vpn.example.com")
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if got != "https://vpn.example.com" {
		t.Fatalf("got %q", got)
	}
}

func TestNormalizeStripsTrailingSlash(t *testing.T) {
	got, err := Normalize("https://vpn.example.com/")
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if got != "https://vpn.example.com" {
		t.Fatalf("got %q", got)
	}
}

func TestNormalizeRejectsEmpty(t *testing.T) {
	if _, err := Normalize(""); !errors.Is(err, ErrMissing) {
		t.Fatalf("expected ErrMissing, got %v", err)
	}
	if _, err := Normalize("   "); !errors.Is(err, ErrMissing) {
		t.Fatalf("expected ErrMissing for whitespace, got %v", err)
	}
}

func TestNormalizeRejectsInvalidVariants(t *testing.T) {
	for _, value := range []string{
		"http://vpn.example.com",
		"https://user:pass@vpn.example.com",
		"https://vpn.example.com?query=1",
		"https://vpn.example.com#fragment",
		"https://vpn.example.com/some/path",
		"not a url \x7f",
	} {
		if _, err := Normalize(value); !errors.Is(err, ErrInvalid) {
			t.Fatalf("Normalize(%q) error = %v, want ErrInvalid", value, err)
		}
	}
}
