package config

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"
)

type Token struct {
	Value     string `json:"value"`
	ExpiresAt string `json:"expires_at"` // yyyy-MM-dd; empty = permanent
}

type Config struct {
	ApiBaseUrl string  `json:"api_base_url"`
	Tokens     []Token `json:"tokens"`
	Port       int     `json:"port"`
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
		Tokens:     []Token{},
		Port:       13335,
	}}

	if data, err := os.ReadFile(cfgPath); err == nil {
		if rewritten, migrated, n := MigrateTokens(data); migrated {
			if err := os.WriteFile(cfgPath, rewritten, 0644); err != nil {
				log.Printf("config: failed to write migrated config: %v", err)
			} else {
				log.Printf("config: migrated %d tokens to new format", n)
				data = rewritten
			}
		}
		if err := json.Unmarshal(data, m.config); err != nil {
			log.Printf("config: failed to parse %s, using defaults: %v", cfgPath, err)
		}
	}

	defaultManager = m
	return nil
}

// MigrateTokens inspects the raw JSON bytes; if the "tokens" field contains any
// legacy string entries, returns the rewritten bytes and (true, n) where n is
// the number of legacy entries converted. The returned slice may be longer or
// shorter than the input. If the file is already in the new format, returns
// (nil, false, 0) and the caller should use the original data.
func MigrateTokens(data []byte) ([]byte, bool, int) {
	var probe struct {
		Tokens json.RawMessage `json:"tokens"`
	}
	if err := json.Unmarshal(data, &probe); err != nil || len(probe.Tokens) == 0 {
		return nil, false, 0
	}
	var raws []json.RawMessage
	if err := json.Unmarshal(probe.Tokens, &raws); err != nil {
		return nil, false, 0
	}
	legacyCount := 0
	out := make([]json.RawMessage, 0, len(raws))
	for _, raw := range raws {
		var s string
		if err := json.Unmarshal(raw, &s); err == nil {
			tok := Token{Value: s, ExpiresAt: ""}
			b, _ := json.Marshal(tok)
			out = append(out, b)
			legacyCount++
		} else {
			out = append(out, raw) // already an object, keep as-is
		}
	}
	if legacyCount == 0 {
		return nil, false, 0
	}
	// Detect mixed file (some legacy + some object) and warn.
	if legacyCount < len(raws) {
		log.Printf("config: WARNING detected mixed legacy+object token entries; normalizing all to object form")
	}
	newTokens, _ := json.Marshal(out)
	var generic map[string]json.RawMessage
	if err := json.Unmarshal(data, &generic); err != nil {
		return nil, false, 0
	}
	generic["tokens"] = newTokens
	rewritten, err := json.MarshalIndent(generic, "", "  ")
	if err != nil {
		return nil, false, 0
	}
	return rewritten, true, legacyCount
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
