package monitor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/pelletier/go-toml/v2"
)

func readConfig(t *testing.T, path string) Object {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var config Object
	if err = toml.Unmarshal(bytes.TrimPrefix(raw, []byte{0xef, 0xbb, 0xbf}), &config); err != nil {
		t.Fatal(err)
	}
	return config
}

func TestOTelMergePreservesClientConfigAcrossTOMLForms(t *testing.T) {
	base := "# keep comment\nmodel = 'test-model'\ndescription = '''\n[otel]\nexporter = 'not-a-real-field'\n'''\n"
	for _, telemetry := range []string{
		"",
		"otel.exporter = 'none'\notel.environment = 'test'\n",
		"otel = { exporter = 'none', environment = 'test' }\n",
		"[otel]\nexporter = 'none'\nenvironment = 'test'\n",
		"[\"otel\".\"exporter\".\"otlp-http\"]\nendpoint = 'http://127.0.0.1:4319/v1/logs'\nprotocol = 'json'\n[otel]\nenvironment = 'test'\n",
	} {
		t.Run(telemetry, func(t *testing.T) {
			raw := []byte(base + telemetry + "[projects.'D:/example']\ntrust_level = 'trusted'\n")
			var before Object
			if err := toml.Unmarshal(raw, &before); err != nil {
				t.Fatal(err)
			}
			otel := clone(obj(before["otel"]))
			otel["exporter"], otel["log_user_prompt"] = localExporter(54321), false
			after, err := replaceOTel(raw, otel)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(after, []byte(base)) || !bytes.Contains(after, []byte("[projects.'D:/example']\ntrust_level = 'trusted'\n")) {
				t.Fatal("unrelated config bytes changed")
			}
			var parsed Object
			if err = toml.Unmarshal(after, &parsed); err != nil || !reflect.DeepEqual(parsed["otel"], otel) {
				t.Fatal("invalid merged telemetry", err)
			}
		})
	}
	crlf := append([]byte{0xef, 0xbb, 0xbf}, []byte("model = 'test'\r\n[otel]\r\nexporter = 'none'\r\n")...)
	after, err := replaceOTel(crlf, Object{"exporter": localExporter(4319), "log_user_prompt": false})
	if err != nil || !bytes.HasPrefix(after, []byte{0xef, 0xbb, 0xbf}) || !bytes.Contains(after, []byte("model = 'test'\r\n")) {
		t.Fatal("BOM/CRLF preservation", err)
	}
}

func TestAutomaticClientSetupPortSyncAndDisable(t *testing.T) {
	s := &Service{store: testStore(t), config: Config{Home: t.TempDir(), PiHome: t.TempDir(), PiExtension: []byte("const endpoint = 'http://127.0.0.1:4319/v1/logs';\n")}}
	path := filepath.Join(s.config.Home, "config.toml")
	original := []byte("# preserve\nmodel = 'test-model'\n[otel]\nexporter = 'none'\nenvironment = 'development'\n")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	for _, port := range []int{4319, 4320, 4320} {
		codex := s.setupCodex(true, port)
		pi := s.setupPi(true, port)
		if codex["state"] != "ready" || pi["state"] != "ready" {
			t.Fatal(codex, pi)
		}
		config := readConfig(t, path)
		otel := obj(config["otel"])
		if config["model"] != "test-model" || otel["environment"] != "development" || otel["log_user_prompt"] != false || !reflect.DeepEqual(otel["exporter"], localExporter(port)) {
			t.Fatal("configuration not preserved/synchronized")
		}
		extension, err := os.ReadFile(filepath.Join(s.config.PiHome, "extensions", "codex-monitor.ts"))
		if err != nil || !strings.Contains(string(extension), fmt.Sprintf(":%d/v1/logs", port)) {
			t.Fatal("Pi endpoint not synchronized", err)
		}
	}
	backup, err := os.ReadFile(path + ".codex-monitor.bak")
	if err != nil || !bytes.Equal(backup, original) {
		t.Fatal("original backup missing", err)
	}
	// Preserve edits made by the user while monitoring was enabled.
	raw, _ := os.ReadFile(path)
	raw = bytes.ReplaceAll(raw, []byte("model = 'test-model'"), []byte("model = 'edited-model'"))
	_ = os.WriteFile(path, raw, 0600)
	if state := s.setupCodex(false, 4320); state["state"] != "disabled" {
		t.Fatal(state)
	}
	config := readConfig(t, path)
	if config["model"] != "edited-model" || obj(config["otel"])["exporter"] != "none" || obj(config["otel"])["log_user_prompt"] != nil {
		t.Fatal("disable did not restore owned fields")
	}
	if state := s.setupPi(false, 4320); state["state"] != "disabled" {
		t.Fatal(state)
	}
	if _, err = os.Stat(filepath.Join(s.config.PiHome, "extensions", "codex-monitor.ts")); !os.IsNotExist(err) {
		t.Fatal("owned Pi extension remains enabled")
	}
}

func TestAutomaticSetupKeepsConflictingOrModifiedFiles(t *testing.T) {
	s := &Service{store: testStore(t), config: Config{Home: t.TempDir(), PiHome: t.TempDir(), PiExtension: []byte("test extension")}}
	path := filepath.Join(s.config.Home, "config.toml")
	for _, original := range []string{"[invalid", "[otel]\nexporter = { otlp-http = { endpoint = 'https://example.test/v1/logs', protocol = 'json' } }\n", "[otel]\nexporter = { otlp-http = { endpoint = 'http://127.0.0.1:4318/v1/logs', protocol = 'json' } }\n"} {
		_ = os.WriteFile(path, []byte(original), 0600)
		if state := s.setupCodex(true, 4319); state["state"] != "error" {
			t.Fatal("conflicting config overwritten", state)
		}
		after, _ := os.ReadFile(path)
		if string(after) != original {
			t.Fatal("conflicting config changed")
		}
	}
	_ = os.WriteFile(path, []byte("model = 'test'\n"), 0600)
	if state := s.setupCodex(true, 4319); state["state"] != "ready" {
		t.Fatal(state)
	}
	raw, _ := os.ReadFile(path)
	modified := bytes.ReplaceAll(raw, []byte("127.0.0.1:4319"), []byte("example.test:4319"))
	_ = os.WriteFile(path, modified, 0600)
	if state := s.setupCodex(false, 4319); state["state"] != "error" {
		t.Fatal("manual exporter edit ignored")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(after, modified) {
		t.Fatal("manually modified exporter overwritten")
	}
	piPath := filepath.Join(s.config.PiHome, "extensions", "codex-monitor.ts")
	_ = os.MkdirAll(filepath.Dir(piPath), 0700)
	_ = os.WriteFile(piPath, []byte("different user extension"), 0600)
	if state := s.setupPi(true, 4319); state["state"] != "error" {
		t.Fatal("unrelated extension overwritten")
	}
}

func TestFreshServiceAutomaticallyConfiguresAndReceivesTTFT(t *testing.T) {
	data, home, pi, claude := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	occupied, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	port := occupied.Addr().(*net.TCPAddr).Port
	store, err := Open(filepath.Join(data, "monitor.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	if !truth(store.settings()["telemetryEnabled"]) {
		t.Fatal("fresh profile does not enable collection")
	}
	_, err = store.saveSettings(Object{"telemetryPort": port, "codexExecutable": filepath.Join(home, "missing.exe")})
	store.Close()
	if err != nil {
		t.Fatal(err)
	}
	service, err := Start(Config{Data: data, Home: home, PiHome: pi, PiExtension: []byte("test extension"), ClaudeHome: claude})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	raw, err := service.Call(ctx, "snapshot", Object{})
	if err != nil {
		t.Fatal(err)
	}
	var snapshot Object
	_ = json.Unmarshal(raw, &snapshot)
	status := obj(snapshot["telemetry"])
	if !truth(status["listening"]) || obj(status["codex"])["state"] != "ready" || obj(status["pi"])["state"] != "ready" || obj(status["claude"])["state"] != "ready" || int(num(status["port"])) == port {
		t.Fatal("automatic startup or port fallback failed", status)
	}
	endpoint := text(obj(obj(obj(readConfig(t, filepath.Join(home, "config.toml"))["otel"])["exporter"])["otlp-http"])["endpoint"])
	settings, _ := os.ReadFile(filepath.Join(claude, "settings.json"))
	if !bytes.Contains(settings, []byte(`"OTEL_EXPORTER_OTLP_LOGS_ENDPOINT": "`+endpoint+`"`)) {
		t.Fatal("Claude Code export not pointed at the collector", string(settings))
	}
	body := telemetryPayload(time.Now(), Object{"event.name": "codex.sse_event", "event.kind": "response.completed", "conversation.id": "test-session", "model": "test-model", "ttft_ms": "1250"})
	response, err := http.Post(endpoint, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal(response.Status)
	}
	raw, err = service.Call(ctx, "snapshot", Object{})
	_ = json.Unmarshal(raw, &snapshot)
	if err != nil || num(obj(snapshot["telemetry"])["ttftSamples"]) != 1 {
		t.Fatal("first-token collection did not connect", err)
	}
	// Turning off Claude Code monitoring also hands its export back.
	if _, err = service.Call(ctx, "settings", Object{"claudeEnabled": false}); err != nil {
		t.Fatal(err)
	}
	if settings, _ = os.ReadFile(filepath.Join(claude, "settings.json")); bytes.Contains(settings, []byte("OTEL")) {
		t.Fatal("Claude Code export left enabled", string(settings))
	}
}

func TestOfflineStartupDoesNotModifyClientFiles(t *testing.T) {
	home, pi, claude := t.TempDir(), t.TempDir(), t.TempDir()
	service, err := Start(Config{Data: t.TempDir(), Home: home, PiHome: pi, PiExtension: []byte("test"), ClaudeHome: claude, Offline: true})
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	_, err = service.Call(context.Background(), "snapshot", Object{})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(home, "config.toml"), filepath.Join(pi, "extensions", "codex-monitor.ts"), filepath.Join(claude, "settings.json")} {
		if _, err = os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("offline mode modified source", path)
		}
	}
}
