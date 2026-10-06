package config_test

import (
	"strings"
	"testing"

	"github.com/mpm/projectsetup/internal/config"
)

func TestReadManifestRejectsUnknownFields(t *testing.T) {
	_, err := config.ReadManifest(strings.NewReader(`{"schemaVersion":1,"unexpected":true}`))
	if err == nil || !strings.Contains(err.Error(), "unknown field") {
		t.Fatalf("ReadManifest() error = %v", err)
	}
}

func TestReadManifestRejectsTrailingJSON(t *testing.T) {
	_, err := config.ReadManifest(strings.NewReader(`{} {}`))
	if err == nil || !strings.Contains(err.Error(), "multiple JSON values") {
		t.Fatalf("ReadManifest() error = %v", err)
	}
}

func TestReadManifestPinsLegacyPostgresVersion(t *testing.T) {
	tests := []struct {
		name     string
		manifest string
		want     string
	}{
		{name: "legacy postgres", manifest: `{"database":"postgres"}`, want: config.LegacyPostgresVersion},
		{name: "recorded postgres", manifest: `{"database":"postgres","postgresVersion":"18"}`, want: "18"},
		{name: "no postgres", manifest: `{"database":"sqlite"}`, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manifest, err := config.ReadManifest(strings.NewReader(tt.manifest))
			if err != nil {
				t.Fatal(err)
			}
			if manifest.PostgresVersion != tt.want {
				t.Fatalf("PostgresVersion = %q, want %q", manifest.PostgresVersion, tt.want)
			}
		})
	}
}
