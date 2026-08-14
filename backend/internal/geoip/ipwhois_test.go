package geoip

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestIPWhoisLookupReturnsLocation(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Host != "ipwho.is" {
			t.Fatalf("unexpected host %q", req.URL.Host)
		}
		if !strings.Contains(req.URL.Path, "8.8.8.8") {
			t.Fatalf("unexpected path %q", req.URL.Path)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body: io.NopCloser(strings.NewReader(`{"success":true,"country":"United States","region":"California","city":"Mountain View","latitude":37.3860517,"longitude":-122.0838511}`)),
			Header: make(http.Header),
		}, nil
	})}

	location, err := NewIPWhoisResolver(client).Lookup(context.Background(), "8.8.8.8")
	if err != nil {
		t.Fatalf("lookup failed: %v", err)
	}
	if location.Country != "United States" || location.City != "Mountain View" {
		t.Fatalf("unexpected location: %+v", location)
	}
	if location.Latitude != 37.3860517 || location.Longitude != -122.0838511 {
		t.Fatalf("unexpected coordinates: %+v", location)
	}
}

func TestIPWhoisLookupAcceptsPostgresINETHostNotation(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/8.8.8.8" {
			t.Fatalf("unexpected path %q", req.URL.Path)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body: io.NopCloser(strings.NewReader(`{"success":true,"country":"United States","region":"California","city":"Mountain View","latitude":37.3860517,"longitude":-122.0838511}`)),
			Header: make(http.Header),
		}, nil
	})}

	if _, err := NewIPWhoisResolver(client).Lookup(context.Background(), "8.8.8.8/32"); err != nil {
		t.Fatalf("lookup with PostgreSQL inet host notation failed: %v", err)
	}
}

func TestIPWhoisLookupRejectsNetworkPrefixWithoutNetworkCall(t *testing.T) {
	called := false
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		called = true
		return nil, nil
	})}

	_, err := NewIPWhoisResolver(client).Lookup(context.Background(), "8.8.8.0/24")
	if err == nil {
		t.Fatal("expected network prefix to be rejected")
	}
	if called {
		t.Fatal("network prefix must not be sent to GeoIP provider")
	}
}

func TestIPWhoisLookupRejectsPrivateIPWithoutNetworkCall(t *testing.T) {
	called := false
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		called = true
		return nil, nil
	})}

	_, err := NewIPWhoisResolver(client).Lookup(context.Background(), "192.168.1.10")
	if err == nil {
		t.Fatal("expected private IP to be rejected")
	}
	if called {
		t.Fatal("private IP must not be sent to GeoIP provider")
	}
}

func TestIPWhoisLookupClassifiesRateLimit(t *testing.T) {
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Body:       io.NopCloser(strings.NewReader(`{"success":false,"message":"Rate limit exceeded"}`)),
			Header:     make(http.Header),
		}, nil
	})}

	_, err := NewIPWhoisResolver(client).Lookup(context.Background(), "1.1.1.1")
	if err != ErrRateLimited {
		t.Fatalf("got %v, want ErrRateLimited", err)
	}
}
