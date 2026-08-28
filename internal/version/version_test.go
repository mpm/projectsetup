package version

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInfo(t *testing.T) {
	originalVersion, originalCommit, originalDate := Version, Commit, Date
	t.Cleanup(func() { Version, Commit, Date = originalVersion, originalCommit, originalDate })
	Version, Commit, Date = "v1.2.3", "1234567890", "2026-08-28T12:00:00Z"

	got := Info()
	for _, want := range []string{"projectsetup v1.2.3", "commit: 1234567", "built: 2026-08-28T12:00:00Z"} {
		if !strings.Contains(got, want) {
			t.Fatalf("Info() = %q, want it to contain %q", got, want)
		}
	}
}

func TestIsNewer(t *testing.T) {
	tests := []struct {
		latest  string
		current string
		want    bool
	}{
		{latest: "v1.2.4", current: "v1.2.3", want: true},
		{latest: "v2.0.0", current: "v1.9.9", want: true},
		{latest: "v1.2.3", current: "v1.2.3"},
		{latest: "v1.2.2", current: "v1.2.3"},
		{latest: "not-a-version", current: "v1.2.3"},
	}
	for _, test := range tests {
		if got := isNewer(test.latest, test.current); got != test.want {
			t.Errorf("isNewer(%q, %q) = %t, want %t", test.latest, test.current, got, test.want)
		}
	}
}

func TestCheckForUpdate(t *testing.T) {
	originalVersion := Version
	t.Cleanup(func() { Version = originalVersion })
	Version = "v1.2.3"

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if got := request.Header.Get("User-Agent"); got != "projectsetup/v1.2.3" {
			t.Errorf("User-Agent = %q", got)
		}
		response.Header().Set("Content-Type", "application/json")
		_, _ = response.Write([]byte(`{"tag_name":"v1.3.0","html_url":"https://example.com/release"}`))
	}))
	defer server.Close()

	result := checkForUpdate(context.Background(), server.Client(), server.URL)
	if result == nil || !result.UpdateAvailable || result.Latest != "v1.3.0" || result.Current != "v1.2.3" {
		t.Fatalf("checkForUpdate() = %#v", result)
	}
}

func TestCheckForUpdateSkipsDevelopmentBuild(t *testing.T) {
	originalVersion := Version
	t.Cleanup(func() { Version = originalVersion })
	Version = "dev"

	if result := checkForUpdate(context.Background(), http.DefaultClient, "::invalid-url"); result != nil {
		t.Fatalf("checkForUpdate() = %#v, want nil", result)
	}
}

func TestCheckForUpdateIgnoresAPIError(t *testing.T) {
	originalVersion := Version
	t.Cleanup(func() { Version = originalVersion })
	Version = "v1.2.3"

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		http.Error(response, "rate limited", http.StatusForbidden)
	}))
	defer server.Close()

	if result := checkForUpdate(context.Background(), server.Client(), server.URL); result != nil {
		t.Fatalf("checkForUpdate() = %#v, want nil", result)
	}
}
