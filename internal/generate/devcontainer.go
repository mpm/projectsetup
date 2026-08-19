package generate

import (
	"fmt"

	"github.com/mpm/projectsetup/internal/config"
)

type devcontainerConfig struct {
	Name              string                    `json:"name"`
	Build             *devcontainerBuild        `json:"build,omitempty"`
	DockerComposeFile string                    `json:"dockerComposeFile,omitempty"`
	Service           string                    `json:"service,omitempty"`
	WorkspaceFolder   string                    `json:"workspaceFolder"`
	WorkspaceMount    string                    `json:"workspaceMount,omitempty"`
	ContainerUser     string                    `json:"containerUser"`
	RemoteUser        string                    `json:"remoteUser"`
	Features          map[string]map[string]any `json:"features"`
	ContainerEnv      map[string]string         `json:"containerEnv"`
	Mounts            []string                  `json:"mounts,omitempty"`
	ForwardPorts      []int                     `json:"forwardPorts,omitempty"`
	PostCreateCommand string                    `json:"postCreateCommand"`
	ShutdownAction    string                    `json:"shutdownAction,omitempty"`
}

type devcontainerBuild struct {
	Dockerfile string `json:"dockerfile"`
	Context    string `json:"context"`
}

func renderDevcontainer(cfg config.Config) ([]byte, error) {
	document := devcontainerConfig{
		Name:            cfg.ProjectName,
		WorkspaceFolder: cfg.Workspace.ContainerPath,
		ContainerUser:   cfg.Container.User,
		RemoteUser:      cfg.Container.User,
		Features:        features(cfg),
		ContainerEnv: map[string]string{
			"PATH": cfg.Container.Home + "/.local/bin:" + cfg.Container.Home + "/.opencode/bin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		},
		Mounts:            aiMounts(cfg),
		ForwardPorts:      append([]int(nil), cfg.Ports...),
		PostCreateCommand: ".devcontainer/scripts/post-create.sh",
	}
	if cfg.Container.UseCompose {
		document.DockerComposeFile = "compose.yaml"
		document.Service = cfg.Container.ServiceName
		document.ShutdownAction = "stopCompose"
		document.ContainerEnv["DB_HOST"] = "postgres"
		document.ContainerEnv["PGHOST"] = "postgres"
		document.ContainerEnv["PGUSER"] = "projectsetup"
		document.ContainerEnv["PGPASSWORD"] = "projectsetup"
		document.ContainerEnv["PGDATABASE"] = cfg.ProjectName
	} else {
		document.Build = &devcontainerBuild{Dockerfile: "Dockerfile", Context: ".."}
		document.WorkspaceMount = fmt.Sprintf("source=${localWorkspaceFolder},target=%s,type=bind", cfg.Workspace.ContainerPath)
	}
	for _, tool := range cfg.AITools {
		if tool == config.AIToolClaude {
			document.ContainerEnv["CLAUDE_CONFIG_DIR"] = cfg.Container.Home + "/.claude"
		}
	}
	return marshalJSON(document)
}

func features(cfg config.Config) map[string]map[string]any {
	result := map[string]map[string]any{
		"ghcr.io/devcontainers/features/github-cli:1": {},
	}
	switch cfg.Preset {
	case config.PresetNode:
		result["ghcr.io/devcontainers/features/node:1"] = map[string]any{"version": cfg.LanguageVersion}
	case config.PresetRails:
		result["ghcr.io/devcontainers/features/node:1"] = map[string]any{"version": "lts"}
		result["ghcr.io/rails/devcontainer/features/activestorage"] = map[string]any{}
		if cfg.Database == config.DatabasePostgres {
			result["ghcr.io/rails/devcontainer/features/postgres-client"] = map[string]any{}
		}
	case config.PresetPython:
		result["ghcr.io/devcontainers/features/python:1"] = map[string]any{"version": cfg.LanguageVersion}
	}
	return result
}

func aiMounts(cfg config.Config) []string {
	var mounts []string
	for _, tool := range cfg.AITools {
		switch tool {
		case config.AIToolOpenCode:
			mounts = append(mounts,
				"source=${localEnv:HOME}/.config/opencode,target=/home/vscode/.config/opencode,type=bind",
				"source=${localEnv:HOME}/.local/share/opencode,target=/home/vscode/.local/share/opencode,type=bind",
				"source=${localEnv:HOME}/.opencode,target=/home/vscode/.opencode,type=bind",
				"source=${localEnv:HOME}/.cache/opencode,target=/home/vscode/.cache/opencode,type=bind",
			)
		case config.AIToolClaude:
			mounts = append(mounts,
				"source=${localEnv:HOME}/.claude,target=/home/vscode/.claude,type=bind",
				"source=${localEnv:HOME}/.local/share/claude,target=/home/vscode/.local/share/claude,type=bind",
			)
		}
	}
	return mounts
}
