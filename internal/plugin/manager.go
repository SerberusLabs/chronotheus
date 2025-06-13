package plugin

import (
	"fmt"
	"log"
	"plugin"
	"sync"
)

// Plugin interface that all plugins must implement
type Plugin interface {
    Init() error
    GetIdentifier() string
    Handle(merged []map[string]interface{}) ([]map[string]interface{}, error)
}

// Manager handles plugin lifecycle
type Manager struct {
    plugins     map[string]Plugin
    pluginPath  string
    mu          sync.RWMutex
}

// Global variables exported for use in other packages
var (
    GlobalPluginManager *Manager
    LoadedPlugins []string
)

// NewManager creates a new plugin manager
func NewManager(pluginPath string) *Manager {
    manager := &Manager{
        plugins:    make(map[string]Plugin),
        pluginPath: pluginPath,
    }
    GlobalPluginManager = manager
    return manager
}

// ProcessPlugins runs a specific plugin on the data
func (m *Manager) ProcessPlugins(merged []map[string]interface{}, requestedPlugin string) ([]map[string]interface{}, error) {
    if requestedPlugin == "" {
        return merged, nil  // No plugin requested, return unmodified data
    }

    m.mu.RLock()
    defer m.mu.RUnlock()

    plugin, exists := m.plugins[requestedPlugin]
    if !exists {
        return merged, fmt.Errorf("plugin %s not found", requestedPlugin)
    }

    processed, err := plugin.Handle(merged)
    if err != nil {
        return merged, fmt.Errorf("plugin %s error: %w", requestedPlugin, err)
    }

    return processed, nil
}

// LoadPlugin loads a plugin from the given path
func (m *Manager) LoadPlugin(path string) error {
    m.mu.Lock()
    defer m.mu.Unlock()

    p, err := plugin.Open(path)
    if err != nil {
        return fmt.Errorf("failed to open plugin: %w", err)
    }

    symPlugin, err := p.Lookup("Plugin")
    if err != nil {
        return fmt.Errorf("plugin does not export 'Plugin' symbol: %w", err)
    }

    chronoPlugin, ok := symPlugin.(Plugin)
    if !ok {
        return fmt.Errorf("plugin does not implement Plugin interface")
    }

    if err := chronoPlugin.Init(); err != nil {
        return fmt.Errorf("failed to initialize plugin: %w", err)
    }

    identifier := chronoPlugin.GetIdentifier()
    m.plugins[identifier] = chronoPlugin
    LoadedPlugins = append(LoadedPlugins, identifier)
    
    log.Printf("Loaded plugin: %s", identifier)
    return nil
}

// UnloadPlugin removes a plugin by its identifier
func (m *Manager) UnloadPlugin(identifier string) {
    m.mu.Lock()
    defer m.mu.Unlock()

    delete(m.plugins, identifier)

    for i, name := range LoadedPlugins {
        if name == identifier {
            LoadedPlugins = append(LoadedPlugins[:i], LoadedPlugins[i+1:]...)
            break
        }
    }

    log.Printf("Unloaded plugin: %s", identifier)
}