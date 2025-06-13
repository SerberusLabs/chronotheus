package proxy

import (
    "github.com/fsnotify/fsnotify"
    "log"
    "path/filepath"
)

func WatchPlugins(pluginPath string) error {
    watcher, err := fsnotify.NewWatcher()
    if err != nil {
        return err
    }

    go func() {
        for {
            select {
            case event, ok := <-watcher.Events:
                if !ok {
                    return
                }

                if filepath.Ext(event.Name) != ".so" {
                    continue
                }

                switch {
                case event.Op&fsnotify.Create == fsnotify.Create:
                    if err := GlobalPluginManager.LoadPlugin(event.Name); err != nil {
                        log.Printf("Error loading plugin %s: %v", event.Name, err)
                    }

                case event.Op&fsnotify.Remove == fsnotify.Remove:
                    identifier := filepath.Base(event.Name)
                    GlobalPluginManager.UnloadPlugin(identifier)
                }

            case err, ok := <-watcher.Errors:
                if !ok {
                    return
                }
                log.Printf("Plugin watcher error: %v", err)
            }
        }
    }()

    return watcher.Add(pluginPath)
}