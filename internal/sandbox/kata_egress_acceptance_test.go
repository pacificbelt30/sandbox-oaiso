package sandbox_test

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/containerd/errdefs"
	"github.com/docker/docker/api/types/container"
	"github.com/pacificbelt30/codex-dock/internal/engine"
	"github.com/pacificbelt30/codex-dock/internal/sandbox"
	"github.com/pacificbelt30/codex-dock/internal/sandboxux"
	"github.com/pacificbelt30/codex-dock/internal/workeregress"
)

//go:embed testdata/sandbox-egress-probe.py
var sandboxEgressProbe string

func TestKataSandboxEgressTUI(t *testing.T) {
	if os.Getenv("CODEX_DOCK_KATA_E2E") != "1" {
		t.Skip("requires disposable Linux/KVM setup acceptance")
	}
	binary, image, proxyImage := os.Getenv("CODEX_DOCK_KATA_TEST_BINARY"), os.Getenv("CODEX_DOCK_KATA_TEST_IMAGE"), os.Getenv("CODEX_DOCK_KATA_PROXY_IMAGE")
	if binary == "" || image == "" || proxyImage == "" {
		t.Fatal("matching CLI, worker image, and proxy image are required")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	root := t.TempDir()
	project := filepath.Join(root, "project")
	if err := os.Mkdir(project, 0700); err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("egress-ui-%d", time.Now().UnixNano())
	manager, err := sandbox.NewManager(sandbox.ManagerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = manager.Close() })
	node, err := engine.NewClient()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = node.Close() })
	previous, err := node.ContainerList(ctx, container.ListOptions{All: true})
	if err != nil {
		t.Fatal(err)
	}
	known := map[string]bool{}
	for _, c := range previous {
		known[c.ID] = true
	}
	networkName := name + "-network"
	peerName := name + "-peer"
	t.Cleanup(func() {
		clean, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		for _, worker := range []string{networkName, peerName} {
			if _, err := node.ContainerInspect(clean, worker); err == nil {
				if err = manager.RemoveContext(clean, worker, true); err != nil {
					t.Error(err)
				}
			} else if !errdefs.IsNotFound(err) {
				t.Error(err)
			}
		}
		current, err := node.ContainerList(clean, container.ListOptions{All: true})
		if err != nil {
			t.Error(err)
			return
		}
		for _, c := range current {
			if known[c.ID] {
				continue
			}
			for _, n := range c.Names {
				if strings.HasPrefix(strings.TrimPrefix(n, "/"), "codex-http-policy-") {
					seconds := 5
					if err = node.ContainerStop(clean, c.ID, container.StopOptions{Timeout: &seconds}); err != nil {
						t.Error(err)
					}
					if err = node.ContainerRemove(clean, c.ID, container.RemoveOptions{Force: true}); err != nil {
						t.Error(err)
					}
				}
			}
		}
	})
	config := filepath.Join(root, "network.toml")
	data := "auto_build = false\nproxy_image = " + strconv.Quote(proxyImage) + "\n[proxy]\nforward_default_deny = true\nforward_allow_domains = [\"=10.255.255.1\"]\nforward_deny_domains = [\"blocked.example\"]\nforward_allow_ports = [80, 443]\n"
	if err = os.WriteFile(config, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	launcher := &sandboxux.CLILauncher{Executable: binary, GlobalArgs: []string{"--config", config}}
	for _, worker := range []string{networkName, peerName} {
		if _, err := launcher.Launch(ctx, sandboxux.LaunchSpec{Name: worker, Project: project, Image: image, Network: "host", Profile: "compact"}); err != nil {
			t.Fatalf("persistent sandbox with credential-free egress: %v", err)
		}
	}
	info, err := node.ContainerInspect(ctx, networkName)
	if err != nil {
		t.Fatal(err)
	}
	if info.Config.Labels["codex-dock.proxy-container-url"] != "" {
		t.Fatal("general egress gained an AI proxy")
	}
	check := exec.CommandContext(ctx, binary, "exec", networkName, "--", "sh", "-ec", `test -n "$HTTP_PROXY"
test -z "${OPENAI_API_KEY:-}${CODEX_TOKEN:-}"
for dest in 10.255.255.1 blocked.example packages.example.invalid; do
  test "$(curl -s -o /dev/null -w '%{http_code}' --max-time 5 -x "$HTTP_PROXY" "http://$dest/")" = 403
done`)
	if out, err := check.CombinedOutput(); err != nil {
		t.Fatalf("credential-free policy enforcement: %v %s", err, out)
	}
	t.Log("verified: blank-command sandbox stays running; host-policy general egress without AI credentials; private IP and host-denied domain refused without external traffic")
	before, err := workeregress.Inspect(ctx, engine.Socket(), networkName)
	if err != nil || before.Generation == 0 {
		t.Fatalf("current egress policy: %+v %v", before, err)
	}
	peer, err := workeregress.Inspect(ctx, engine.Socket(), peerName)
	if err != nil || peer.Proxy != before.Proxy {
		t.Fatalf("worker did not share the general proxy: %+v %v", peer, err)
	}
	command := exec.CommandContext(ctx, "python3", "-c", terminalScreenProbe+"\n"+sandboxEgressProbe, binary, project, config, networkName, peerName, strconv.FormatUint(before.Generation, 10))
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("public TUI egress review: %v\n%s", err, out)
	} else {
		t.Log(strings.TrimSpace(string(out)))
	}
}
