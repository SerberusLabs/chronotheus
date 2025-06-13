package proxy

import (
    "fmt"
    "log"
    "plugin"
    "sync"
)

// Plugin interface that all plugins must implement
type ChronoPlugin interface {
    Init() error
    GetIdentifier() string
    Handle(merged []map[string]interface{}) ([]map[string]interface{}, error)
}

// PluginManager handles plugin lifecycle
type PluginManager struct {
    plugins     map[string]ChronoPlugin
    pluginPath  string
    mu          sync.RWMutex
}

var (
    // Global plugin manager instance
    GlobalPluginManager *PluginManager
    // Global slice of loaded plugin names
    LoadedPlugins []string
)

func NewPluginManager(pluginPath string) *PluginManager {
    return &PluginManager{
        plugins:    make(map[string]ChronoPlugin),
        pluginPath: pluginPath,
    }
}

func (pm *PluginManager) LoadPlugin(path string) error {
    pm.mu.Lock()
    defer pm.mu.Unlock()

    // Load the plugin
    p, err := plugin.Open(path)
    if err != nil {
        return fmt.Errorf("failed to open plugin: %w", err)
    }

    // Look up the plugin symbol
    symPlugin, err := p.Lookup("Plugin")
    if err != nil {
        return fmt.Errorf("plugin does not export 'Plugin' symbol: %w", err)
    }

    // Assert the plugin interface
    chronoPlugin, ok := symPlugin.(ChronoPlugin)
    if !ok {
        return fmt.Errorf("plugin does not implement ChronoPlugin interface")
    }

    // Initialize the plugin
    if err := chronoPlugin.Init(); err != nil {
        return fmt.Errorf("failed to initialize plugin: %w", err)
    }

    // Store the plugin
    identifier := chronoPlugin.GetIdentifier()
    pm.plugins[identifier] = chronoPlugin

    // Update global loaded plugins slice
    LoadedPlugins = append(LoadedPlugins, identifier)
    
    log.Printf("Loaded plugin: %s", identifier)
    return nil
}

func (pm *PluginManager) UnloadPlugin(identifier string) {
    pm.mu.Lock()
    defer pm.mu.Unlock()

    delete(pm.plugins, identifier)

    // Update global loaded plugins slice
    for i, name := range LoadedPlugins {
        if name == identifier {
            LoadedPlugins = append(LoadedPlugins[:i], LoadedPlugins[i+1:]...)
            break
        }
    }

    log.Printf("Unloaded plugin: %s", identifier)
}

func (pm *PluginManager) ProcessPlugins(merged []map[string]interface{}) ([]map[string]interface{}, error) {
    pm.mu.RLock()
    defer pm.mu.RUnlock()

    var err error
    for _, p := range pm.plugins {
        merged, err = p.Handle(merged)
        if err != nil {
            return merged, fmt.Errorf("plugin %s error: %w", p.GetIdentifier(), err)
        }
    }
    return merged, nil
}