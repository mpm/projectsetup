package generate

import (
	"maps"

	"github.com/mpm/projectsetup/internal/config"
	"github.com/mpm/projectsetup/internal/presets"
)

type devcontainerConfig struct {
	Name              string                    `json:"name"`
	DockerComposeFile string                    `json:"dockerComposeFile,omitempty"`
	Service           string                    `json:"service,omitempty"`
	WorkspaceFolder   string                    `json:"workspaceFolder"`
	ContainerUser     string                    `json:"containerUser"`
	RemoteUser        string                    `json:"remoteUser"`
	Features          map[string]map[string]any `json:"features"`
	ContainerEnv      map[string]string         `json:"containerEnv"`
	Mounts            []string                  `json:"mounts,omitempty"`
	ForwardPorts      []int                     `json:"forwardPorts,omitempty"`
	PostCreateCommand string                    `json:"postCreateCommand"`
	ShutdownAction    string                    `json:"shutdownAction,omitempty"`
}

func renderDevcontainer(cfg config.Config, resolved presets.Resolved) ([]byte, error) {
	document := devcontainerConfig{
		Name:              cfg.ProjectName,
		DockerComposeFile: "compose.yaml",
		Service:           cfg.Container.ServiceName,
		WorkspaceFolder:   cfg.Workspace.ContainerPath,
		ContainerUser:     cfg.Container.User,
		RemoteUser:        cfg.Container.User,
		Features:          resolved.DevcontainerFeatures(),
		ContainerEnv:      map[string]string{},
		Mounts:            aiMounts(cfg),
		ForwardPorts:      append([]int(nil), cfg.Ports...),
		PostCreateCommand: ".devcontainer/scripts/post-create.sh",
		ShutdownAction:    "stopCompose",
	}
	// Definitions cannot set the reserved keys written below.
	maps.Copy(document.ContainerEnv, resolved.Env)
	document.ContainerEnv["PATH"] = config.ContainerPath(cfg.Container.Home, resolved)
	for _, tool := range cfg.AITools {
		if tool == config.AIToolCodex {
			document.ContainerEnv["CODEX_HOME"] = cfg.Container.Home + "/.codex"
		}
		if tool == config.AIToolClaude {
			document.ContainerEnv["CLAUDE_CONFIG_DIR"] = cfg.Container.Home + "/.claude"
		}
	}
	return marshalJSON(document)
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
		case config.AIToolCodex:
			mounts = append(mounts,
				"source=${localEnv:HOME}/.local/share/codex,target=/home/vscode/.local/share/codex,type=bind")
		case config.AIToolClaude:
			mounts = append(mounts,
				"source=${localEnv:HOME}/.claude,target=/home/vscode/.claude,type=bind",
				"source=${localEnv:HOME}/.local/share/claude,target=/home/vscode/.local/share/claude,type=bind",
			)
		}
	}
	return mounts
}
