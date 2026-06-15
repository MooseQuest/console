package plugin

import (
	"fmt"
	"os"
	"os/exec"

	hclog "github.com/hashicorp/go-hclog"
	goplugin "github.com/hashicorp/go-plugin"

	"github.com/moosequest/console/internal/store"
)

// pluginStore wraps the gRPC store client so that Close tears down the plugin
// subprocess. Callers treat it like any store.Store.
type pluginStore struct {
	store.Store
	client *goplugin.Client
}

// Close kills the plugin subprocess.
func (p *pluginStore) Close() error {
	p.client.Kill()
	return nil
}

// LoadStore launches the store-plugin executable at path and returns a
// store.Store backed by it over gRPC. Closing the returned store stops the
// subprocess. The plugin inherits the host's environment (so configuration such
// as CONSOLE_DB reaches it).
func LoadStore(path string) (store.Store, error) {
	client := goplugin.NewClient(&goplugin.ClientConfig{
		HandshakeConfig:  Handshake,
		Plugins:          PluginSet(nil),
		Cmd:              exec.Command(path),
		AllowedProtocols: []goplugin.Protocol{goplugin.ProtocolGRPC},
		Logger:           hclog.New(&hclog.LoggerOptions{Name: "console-plugin", Level: hclog.Warn, Output: os.Stderr}),
	})

	rpc, err := client.Client()
	if err != nil {
		client.Kill()
		return nil, fmt.Errorf("start store plugin %q: %w", path, err)
	}
	raw, err := rpc.Dispense(StorePluginName)
	if err != nil {
		client.Kill()
		return nil, fmt.Errorf("dispense store plugin: %w", err)
	}
	st, ok := raw.(store.Store)
	if !ok {
		client.Kill()
		return nil, fmt.Errorf("store plugin returned %T, want store.Store", raw)
	}
	return &pluginStore{Store: st, client: client}, nil
}

// Serve runs a store implementation as a plugin. Plugin executables call this
// from main; it blocks until the host disconnects.
func Serve(impl store.Store) {
	goplugin.Serve(&goplugin.ServeConfig{
		HandshakeConfig: Handshake,
		Plugins:         PluginSet(impl),
		GRPCServer:      goplugin.DefaultGRPCServer,
	})
}
