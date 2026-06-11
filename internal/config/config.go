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
	Label     string `json:"label"`
	ExpiresAt string `json:"expires_at"` // yyyy-MM-dd; empty = permanent
}

type Config struct {
	ApiBaseUrl           string  `json:"api_base_url"`
	Tokens               []Token `json:"tokens"`
	Port                 int     `json:"port"`
	HistoryRetentionDays int     `json:"history_retention_days"`
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

	// Track whether the config file existed when Init ran. Used to decide
	// whether to inject the history_retention_days default: a fresh install
	// (no file) is treated the same as "file exists but lacks the key".
	fileExisted := false
	if data, err := os.ReadFile(cfgPath); err == nil {
		fileExisted = true
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

	// Backfill: empty token label → value's first 8 chars + "..."
	_, _ = m.loadRawJson() // probed for its side-effect; result no longer needed
	labelChanged := false
	for i := range m.config.Tokens {
		if m.config.Tokens[i].Label == "" {
			v := m.config.Tokens[i].Value
			if len(v) > 8 {
				v = v[:8]
			}
			m.config.Tokens[i].Label = v + "..."
			labelChanged = true
		}
	}

	// Clamp HistoryRetentionDays into the valid [1, 60] range. The legacy
	// "0 = permanent" semantics and any value > 60 are replaced with 60.
	// The "key absent" path still injects 60 as the default for fresh installs.
	retentionChanged := false
	if m.config.HistoryRetentionDays < 1 || m.config.HistoryRetentionDays > 60 {
		m.config.HistoryRetentionDays = 60
		retentionChanged = true
	}
	// For fresh installs (no file at all), if the default injection above didn't
	// already move us off 0, force 60.
	if !fileExisted && m.config.HistoryRetentionDays == 0 {
		m.config.HistoryRetentionDays = 60
		retentionChanged = true
	}

	if labelChanged || retentionChanged {
		data, _ := json.MarshalIndent(m.config, "", "  ")
		if err := os.WriteFile(cfgPath, data, 0644); err != nil {
			log.Printf("config: failed to write backfilled config: %v", err)
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

// loadRawJson reads the raw config file and returns its top-level keys as
// json.RawMessage values. Used by Init's backfill to probe whether a key
// is explicitly set in the file (vs. simply defaulting to zero).
func (m *Manager) loadRawJson() (map[string]json.RawMessage, error) {
	data, err := os.ReadFile(m.path)
	if err != nil {
		return nil, err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	return raw, nil
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
