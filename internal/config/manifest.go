package config

type Manifest struct {
	SchemaVersion   int            `json:"schemaVersion"`
	ProjectName     string         `json:"projectName"`
	Preset          Preset         `json:"preset"`
	Database        Database       `json:"database"`
	AITools         []AITool       `json:"aiTools"`
	PackageManager  PackageManager `json:"packageManager,omitempty"`
	LanguageVersion string         `json:"languageVersion"`
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
		AITools:         append([]AITool(nil), config.AITools...),
		PackageManager:  config.PackageManager,
		LanguageVersion: config.LanguageVersion,
		Ports:           append([]int(nil), config.Ports...),
		SystemPackages:  append([]string(nil), config.SystemPackages...),
		GeneratedBy:     "projectsetup",
	}
}
