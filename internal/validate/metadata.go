package validate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/mpm/projectsetup/internal/presets"
)

type inspectedImage struct {
	Config struct {
		User    string
		Env     []string
		Labels  map[string]string
		Volumes map[string]any
	}
}

func inspectImage(image string, runner Runner, fail func(string, ...any)) (inspectedImage, bool) {
	output, err := runner.Run("docker", "image", "inspect", image)
	if err != nil {
		fail("inspect image %q for Dev Container metadata: %s; ensure Docker is running and pull/build the image locally before retrying", image, commandFailure(err, output))
		return inspectedImage{}, false
	}
	var images []inspectedImage
	if err := json.Unmarshal(output, &images); err != nil || len(images) != 1 {
		fail("inspect image %q: expected one Docker image inspection document", image)
		return inspectedImage{}, false
	}
	return images[0], true
}

// The CLI reads FROM metadata before building and may re-emit it even if a
// project Dockerfile resets the label. Reject project behavior in the artifact
// itself; overriding a hook in devcontainer.json does not remove inherited hooks.
func validateBaseMetadata(root, devDir, image string, want devcontainerDocument, runner Runner, add func(Severity, string, string, ...any)) bool {
	valid := true
	fail := func(format string, args ...any) {
		valid = false
		add(Error, ".devcontainer/Dockerfile", format, args...)
	}
	inspected, ok := inspectImage(image, runner, fail)
	if !ok {
		return false
	}
	if len(inspected.Config.Volumes) > 0 {
		fail("base image %q declares Docker VOLUME mounts; rebuild a toolchain-only image without inherited project volumes", image)
	}
	label := inspected.Config.Labels["devcontainer.metadata"]
	if label != "" {
		var entries []map[string]json.RawMessage
		if err := json.Unmarshal([]byte(label), &entries); err != nil {
			// Older prebuilt images may carry the single-object form.
			var entry map[string]json.RawMessage
			if err := json.Unmarshal([]byte(label), &entry); err != nil || entry == nil {
				fail("base image %q has invalid devcontainer.metadata; rebuild it with valid object/array metadata", image)
				return false
			}
			entries = []map[string]json.RawMessage{entry}
		}
		if entries == nil {
			fail("base image %q devcontainer.metadata must be an object or array, not null", image)
		}
		for index, entry := range entries {
			if entry == nil {
				fail("base image %q devcontainer.metadata[%d] must be an object", image, index)
				continue
			}
			for _, key := range []string{"initializeCommand", "onCreateCommand", "updateContentCommand", "postCreateCommand", "postStartCommand", "postAttachCommand", "mounts"} {
				var value any
				if raw := entry[key]; raw != nil {
					_ = json.Unmarshal(raw, &value)
				}
				if !emptyCommand(value) {
					fail("base image %q devcontainer.metadata[%d].%s contains inherited project behavior; rebuild a toolchain-only image with that metadata removed before consumption", image, index, key)
				}
			}
		}
	}
	if valid {
		probeMetadata(root, devDir, image, false, want, runner, fail)
	}
	return valid
}

func validateBuiltMetadata(root, devDir string, output []byte, want devcontainerDocument, runner Runner, add func(Severity, string, string, ...any)) (string, bool) {
	valid := true
	fail := func(format string, args ...any) {
		valid = false
		add(Error, ".devcontainer/devcontainer.json", format, args...)
	}
	var result struct {
		ImageName json.RawMessage `json:"imageName"`
	}
	var image string
	var imageNames []string
	err := json.Unmarshal(output, &result)
	if err == nil {
		if json.Unmarshal(result.ImageName, &image) != nil && json.Unmarshal(result.ImageName, &imageNames) == nil && len(imageNames) > 0 {
			image = imageNames[0]
		}
	}
	if err != nil || image == "" {
		fail("inspect built Dev Container metadata: build did not return an imageName string or array")
		return "", false
	}
	inspected, ok := inspectImage(image, runner, fail)
	if !ok {
		return "", false
	}
	if len(inspected.Config.Volumes) > 0 {
		fail("built image %q declares Docker VOLUME mounts; remove them before mount-free verification", image)
		return image, false
	}
	if inspected.Config.User != "vscode" {
		fail("built image %q effective user is %q; expected vscode", image, inspected.Config.User)
	}
	for _, entry := range inspected.Config.Env {
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		if key == "HOME" && value != "/home/vscode" {
			fail("built image %q HOME is %q; expected /home/vscode", image, value)
		}
		if key == "USER" && value != "vscode" {
			fail("built image %q USER is %q; expected vscode", image, value)
		}
	}
	probeMetadata(root, devDir, image, true, want, runner, fail)
	return image, valid
}

// A stopped, mount-free probe uses the CLI's own merge rules. Labels on a final
// image probe reproduce how up reads its recorded configuration, rather than
// appending the same project's lifecycle commands a second time. Base probes
// deliberately have no project labels: current configuration must be merged.
func probeMetadata(root, devDir, image string, built bool, want devcontainerDocument, runner Runner, fail func(string, ...any)) {
	args := []string{"create"}
	configPath := filepath.Join(devDir, "devcontainer.json")
	if built {
		args = append(args, "--label", "devcontainer.local_folder="+root, "--label", "devcontainer.config_file="+configPath)
	}
	args = append(args, "--entrypoint", "/bin/true", image)
	output, err := runner.Run("docker", args...)
	if err != nil {
		fail("create stopped metadata probe for %q: %s", image, commandFailure(err, output))
		return
	}
	id := strings.TrimSpace(string(output))
	if id == "" || strings.ContainsAny(id, " \t\r\n") {
		fail("create stopped metadata probe: Docker returned no valid container ID")
		return
	}
	defer func() {
		if output, err := runner.Run("docker", "rm", "--volumes", id); err != nil {
			fail("remove stopped metadata probe %s: %s; remove it with docker rm --volumes %s", id, commandFailure(err, output), id)
		}
	}()
	output, err = runner.Run("devcontainer", "read-configuration", "--workspace-folder", root, "--config", configPath, "--container-id", id, "--include-merged-configuration")
	if err != nil {
		fail("inspect effective built metadata: %s", commandFailure(err, output))
		return
	}
	validateMergedMetadata(output, want, fail)
}

func validateMergedMetadata(output []byte, want devcontainerDocument, fail func(string, ...any)) {
	var result struct {
		Merged map[string]json.RawMessage `json:"mergedConfiguration"`
	}
	if err := json.Unmarshal(output, &result); err != nil || result.Merged == nil {
		fail("inspect effective built metadata: missing or invalid mergedConfiguration")
		return
	}
	var effective devcontainerDocument
	data, _ := json.Marshal(result.Merged)
	if err := json.Unmarshal(data, &effective); err != nil {
		fail("inspect effective built metadata: parse merged configuration: %v", err)
		return
	}
	if effective.ContainerUser != "vscode" || effective.RemoteUser != "vscode" {
		fail("effective containerUser and remoteUser must both be vscode")
	}
	if want.UpdateRemoteUserUID != nil && !*want.UpdateRemoteUserUID && (effective.UpdateRemoteUserUID == nil || *effective.UpdateRemoteUserUID) {
		fail("effective updateRemoteUserUID must be false for the fixed-ID image contract")
	}
	if raw := result.Merged["updateRemoteUserUID"]; raw != nil && want.UpdateRemoteUserUID == nil {
		var update bool
		if err := json.Unmarshal(raw, &update); err != nil || !update {
			fail("effective updateRemoteUserUID disables or invalidates the default UID adjustment policy; remove the inherited setting or select an explicit image.ownership fixed-ID contract")
		}
	}
	for _, key := range sortedKeys(want.ContainerEnv) {
		if effective.ContainerEnv[key] != want.ContainerEnv[key] {
			fail("effective containerEnv.%s is %q; expected %q", key, effective.ContainerEnv[key], want.ContainerEnv[key])
		}
	}
	for _, key := range []string{"HOME", "USER"} {
		if value, ok := effective.ContainerEnv[key]; ok && value != map[string]string{"HOME": "/home/vscode", "USER": "vscode"}[key] {
			fail("effective containerEnv.%s conflicts with the vscode user/home", key)
		}
	}
	if len(effective.Features) != len(want.Features) || (len(want.Features) > 0 && !reflect.DeepEqual(effective.Features, want.Features)) {
		fail("effective feature requests differ from the generated installation plan; baked feature IDs are metadata, not installer requests")
	}
	wantMounts := make([]string, len(want.Mounts))
	home, err := os.UserHomeDir()
	if err != nil && len(want.Mounts) > 0 {
		fail("resolve expected AI mounts for effective metadata: %v", err)
	}
	for index, mount := range want.Mounts {
		mount = strings.ReplaceAll(mount, "${localEnv:HOME}", home)
		source := mountField(mount, "source")
		if !strings.Contains(source, "${localEnv:") {
			wantMounts[index] = mount
			continue
		}
		resolvedSource, resolveErr := presets.ResolveHostSource(source, os.LookupEnv)
		if resolveErr != nil {
			fail("resolve expected bind mount metadata: %v", resolveErr)
		} else {
			mount = strings.Replace(mount, "source="+source, "source="+resolvedSource, 1)
		}
		wantMounts[index] = mount
	}
	if len(effective.Mounts) != len(wantMounts) || (len(wantMounts) > 0 && !reflect.DeepEqual(effective.Mounts, wantMounts)) {
		fail("effective mounts differ from the generated mounts; remove inherited project or credential mounts")
	}
	var remoteEnv map[string]*string
	if raw := result.Merged["remoteEnv"]; raw != nil {
		if err := json.Unmarshal(raw, &remoteEnv); err != nil {
			fail("effective remoteEnv is invalid: %v", err)
		} else {
			for _, key := range []string{"HOME", "USER", "PATH", "CODEX_HOME", "CLAUDE_CONFIG_DIR"} {
				if value, ok := remoteEnv[key]; ok {
					expected := want.ContainerEnv[key]
					if key == "HOME" {
						expected = "/home/vscode"
					}
					if key == "USER" {
						expected = "vscode"
					}
					if value == nil || (*value != expected && *value != "${containerEnv:"+key+"}") {
						fail("effective remoteEnv.%s conflicts with the direct-exec environment", key)
					}
				}
			}
		}
	}
	for _, key := range []string{"onCreateCommands", "updateContentCommands", "postCreateCommands", "postStartCommands", "postAttachCommands"} {
		var commands []any
		if err := json.Unmarshal(result.Merged[key], &commands); err != nil {
			fail("effective %s is missing or invalid: %v", key, err)
			continue
		}
		if key == "postCreateCommands" {
			if len(commands) != 1 || commands[0] != want.PostCreateCommand {
				fail("effective postCreateCommands must run only %q once; remove inherited project setup from the image or explicit features", want.PostCreateCommand)
			}
		} else {
			for _, command := range commands {
				if !emptyCommand(command) {
					fail("effective %s contains unexpected lifecycle behavior; remove inherited project setup from the image or explicit features", key)
					break
				}
			}
		}
	}
}

func emptyCommand(command any) bool {
	switch value := command.(type) {
	case nil:
		return true
	case string:
		return value == ""
	case []any:
		return len(value) == 0
	case map[string]any:
		for _, child := range value {
			if !emptyCommand(child) {
				return false
			}
		}
		return true
	}
	return false
}
