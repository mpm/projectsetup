package config_test

import (
	"strings"
	"testing"

	"projectsetup/internal/config"
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
