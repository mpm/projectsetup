package config

import (
	"encoding/json"
	"fmt"
	"io"
)

type Manifest struct {
	SchemaVersion   int            `json:"schemaVersion"`
	ProjectName     string         `json:"projectName"`
	Preset          Preset         `json:"preset"`
	Database        Database       `json:"database"`
	AITools         []AITool       `json:"aiTools"`
	PackageManager  PackageManager `json:"packageManager,omitempty"`
	LanguageVersion string         `json:"languageVersion"`
	PostgresVersion string         `json:"postgresVersion,omitempty"`
	Ports           []int          `json:"ports"`
	SystemPackages  []string       `json:"systemPackages"`
	GeneratedBy     string         `json:"generatedBy"`
}

func NewManifest(config Config) Manifest {
	return Manifest{
		SchemaVersion:   config.SchemaVersion,
		ProjectName:     config.ProjectName,
		Preset:          config.Preset,
		Database:        config.Database,
		AITools:         append(make([]AITool, 0, len(config.AITools)), config.AITools...),
		PackageManager:  config.PackageManager,
		LanguageVersion: config.LanguageVersion,
		PostgresVersion: config.PostgresVersion,
		Ports:           append(make([]int, 0, len(config.Ports)), config.Ports...),
		SystemPackages:  append(make([]string, 0, len(config.SystemPackages)), config.SystemPackages...),
		GeneratedBy:     "projectsetup",
	}
}

func ReadManifest(reader io.Reader) (Manifest, error) {
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("parse manifest: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return Manifest{}, fmt.Errorf("parse manifest: multiple JSON values")
		}
		return Manifest{}, fmt.Errorf("parse manifest: %w", err)
	}
	if manifest.Database == DatabasePostgres && manifest.PostgresVersion == "" {
		manifest.PostgresVersion = LegacyPostgresVersion
	}
	return manifest, nil
}
