package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

type Config struct {
	ApiBaseUrl string   `json:"api_base_url"`
	Tokens     []string `json:"tokens"`
	Port       int      `json:"port"`
}

type Manager struct {
	path   string
	config *Config
	mu     sync.RWMutex
}

var defaultManager *Manager

var ExeDir string

func Init(exePath string, binName string) error {
	ExeDir = filepath.Dir(exePath)
	cfgPath := filepath.Join(ExeDir, binName+".json")
	m := &Manager{path: cfgPath, config: &Config{
		ApiBaseUrl: "http://127.0.0.1:2022",
		Tokens:     []string{},
		Port:       13335,
	}}

	if data, err := os.ReadFile(cfgPath); err == nil {
		json.Unmarshal(data, m.config)
	}

	defaultManager = m
	return nil
}

func Get() *Config {
	if defaultManager == nil {
		return &Config{ApiBaseUrl: "http://127.0.0.1:2022", Port: 13335}
	}
	defaultManager.mu.RLock()
	defer defaultManager.mu.RUnlock()
	return defaultManager.config
}

func Save(c *Config) error {
	if defaultManager == nil {
		return nil
	}
	defaultManager.mu.Lock()
	defer defaultManager.mu.Unlock()
	data, _ := json.MarshalIndent(c, "", "  ")
	defaultManager.config = c
	return os.WriteFile(defaultManager.path, data, 0644)
}