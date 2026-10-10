package generate

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/mpm/projectsetup/internal/config"
	"github.com/mpm/projectsetup/internal/presets"
	"github.com/mpm/projectsetup/internal/validate"
)

// This opt-in test talks to the caller's real compositor. It binds only the
// selected socket plus a test client, and never starts a display server.
func TestWaylandIntegration(t *testing.T) {
	if os.Getenv("PROJECTSETUP_WAYLAND_TESTS") != "1" {
		t.Skip("set PROJECTSETUP_WAYLAND_TESTS=1 and PROJECTSETUP_WAYLAND_IMAGE to a local image with matching vscode IDs")
	}
	if runtime.GOOS != "linux" {
		t.Fatal("Wayland integration requires Linux")
	}
	image := os.Getenv("PROJECTSETUP_WAYLAND_IMAGE")
	if image == "" {
		t.Fatal("set PROJECTSETUP_WAYLAND_IMAGE to a local vscode image")
	}
	input := waylandInput(t)
	input.Root = t.TempDir()
	cfg, err := config.Normalize(input)
	if err != nil {
		t.Fatal(err)
	}
	if err := Write(input.Root, cfg, false); err != nil {
		t.Fatal(err)
	}
	if diagnostics := validate.Check(input.Root, validate.Options{CheckHostMounts: true, External: true}); validate.ErrorCount(diagnostics) > 0 {
		t.Fatal(diagnostics)
	}
	resolved, err := config.Resolve(cfg)
	if err != nil {
		t.Fatal(err)
	}
	source, err := presets.ResolveHostSource(resolved.Mounts[0].Source, os.LookupEnv)
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	clientSource := filepath.Join(work, "client.go")
	binary := filepath.Join(work, "wayland-client")
	// wl_display.sync(callback=2), then require wl_callback.done. Native Linux
	// byte order on supported amd64/arm64 is little endian.
	if runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64" {
		t.Fatal("test Wayland client supports amd64/arm64 only")
	}
	client := `package main
import("encoding/binary";"fmt";"io";"net";"os";"time")
func main(){if os.Getuid()==0{panic("expected nonroot vscode")};c,e:=net.DialTimeout("unix",os.Getenv("WAYLAND_DISPLAY"),5*time.Second);if e!=nil{panic(e)};defer c.Close();c.SetDeadline(time.Now().Add(5*time.Second));request:=make([]byte,12);binary.LittleEndian.PutUint32(request,1);binary.LittleEndian.PutUint32(request[4:],12<<16);binary.LittleEndian.PutUint32(request[8:],2);if _,e=c.Write(request);e!=nil{panic(e)};reply:=make([]byte,12);if _,e=io.ReadFull(c,reply);e!=nil{panic(e)};if binary.LittleEndian.Uint32(reply)!=2||binary.LittleEndian.Uint32(reply[4:])!=12<<16{panic(fmt.Sprintf("unexpected callback %x",reply))};fmt.Printf("Wayland sync succeeded as %d:%d via %s\n",os.Getuid(),os.Getgid(),os.Getenv("WAYLAND_DISPLAY"))}
`
	if err := os.WriteFile(clientSource, []byte(client), 0o600); err != nil {
		t.Fatal(err)
	}
	build := exec.Command("go", "build", "-o", binary, clientSource)
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build Wayland client: %v: %s", err, output)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	args := []string{"create", "--network", "none", "--no-healthcheck", "--user", "vscode", "--env", "WAYLAND_DISPLAY=" + resolved.Env["WAYLAND_DISPLAY"], "--mount", "type=bind,source=" + source + ",target=" + resolved.Mounts[0].Target + ",readonly", "--mount", "type=bind,source=" + binary + ",target=/wayland-client,readonly", "--entrypoint", "/bin/sleep", image, "infinity"}
	output, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("create: %v: %s", err, output)
	}
	id := strings.TrimSpace(string(output))
	defer func() {
		if out, err := exec.Command("docker", "rm", "--force", "--volumes", id).CombinedOutput(); err != nil {
			t.Errorf("remove owned Wayland probe %s: %v: %s", id, err, out)
		}
	}()
	if output, err := exec.CommandContext(ctx, "docker", "start", id).CombinedOutput(); err != nil {
		t.Fatalf("start: %v: %s", err, output)
	}
	if output, err := exec.CommandContext(ctx, "docker", "exec", "--user", "vscode", id, "/wayland-client").CombinedOutput(); err != nil {
		t.Fatalf("non-login Wayland sync: %v: %s", err, output)
	} else {
		t.Log(strings.TrimSpace(string(output)))
	}
}
