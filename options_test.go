package monitor

import (
	"testing"
)

func TestOptions(t *testing.T) {
	m := &Monitor{}

	WithConfigFile("/etc/config.toml")(m)
	if m.cfgPath != "/etc/config.toml" {
		t.Errorf("cfgPath = %q, want %q", m.cfgPath, "/etc/config.toml")
	}

	cfg := DefaultConfig()
	WithConfig(cfg)(m)
	if m.cfg != cfg {
		t.Error("cfg should be set")
	}

	WithEcho(true)(m)
	if !m.echoMode {
		t.Error("echoMode should be true")
	}

	WithRunOnce(true)(m)
	if !m.runOnce {
		t.Error("runOnce should be true")
	}

	logger, _ := NewLogger("info")
	WithLogger(logger)(m)
	if m.logger != logger {
		t.Error("logger should be set")
	}

	backend := &mockBackend{name: "test"}
	WithBackend(backend)(m)
	if len(m.backends) != 1 {
		t.Errorf("backends count = %d, want 1", len(m.backends))
	}

	reloadFn := func(path string) (*Config, error) { return nil, nil }
	WithReloadFunc(reloadFn)(m)
	if m.reloadFn == nil {
		t.Error("reloadFn should be set")
	}
}
