package generate

import (
	"maps"
	"strings"

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
		Features:          features(resolved),
		ContainerEnv:      map[string]string{},
		Mounts:            aiMounts(cfg),
		ForwardPorts:      append([]int(nil), cfg.Ports...),
		PostCreateCommand: ".devcontainer/scripts/post-create.sh",
		ShutdownAction:    "stopCompose",
	}
	// Definitions cannot set the reserved keys written below.
	maps.Copy(document.ContainerEnv, resolved.Env)
	document.ContainerEnv["PATH"] = containerPath(cfg, resolved)
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

func features(resolved presets.Resolved) map[string]map[string]any {
	result := map[string]map[string]any{
		"ghcr.io/devcontainers/features/github-cli:1": {},
	}
	maps.Copy(result, resolved.Features)
	return result
}

// containerPath is set in containerEnv because dworm exec does not apply
// remoteEnv. It replaces PATH changes made by features, so definitions list
// the directories their features install into.
func containerPath(cfg config.Config, resolved presets.Resolved) string {
	paths := []string{
		cfg.Container.Home + "/.local/bin",
		cfg.Container.Home + "/.opencode/bin",
	}
	paths = append(paths, resolved.Path...)
	paths = append(paths, "/usr/local/sbin", "/usr/local/bin", "/usr/sbin", "/usr/bin", "/sbin", "/bin")
	return strings.Join(paths, ":")
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
