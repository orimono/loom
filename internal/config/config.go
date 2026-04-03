package config

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

type Duration time.Duration

func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	dur, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	*d = Duration(dur)
	return nil
}

type Config struct {
	Addr         string   `json:"addr"`
	DatabaseURL  string   `json:"database_url"`
	PingInterval Duration `json:"ping_interval"`
	PongTimeout  Duration `json:"pong_timeout"`
	WriteTimeout Duration `json:"write_timeout"`
}

func Load() (*Config, error) {
	cfgPath := os.Getenv("LOOM_CONFIG_PATH")
	if cfgPath == "" {
		home := os.Getenv("LOOM_HOME")
		if home == "" {
			home = "."
		}
		cfgPath = filepath.Join(home, "config.json")
	}

	absPath, err := filepath.Abs(cfgPath)
	if err != nil {
		return nil, err
	}

	content, err := os.ReadFile(absPath)
	if err != nil {
		return nil, err
	}

	cfg := &Config{}
	if err := json.Unmarshal(content, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func MustLoad() *Config {
	cfg, err := Load()
	if err != nil {
		slog.Error("failed to load config", "err", err)
		panic("critical configuration error")
	}
	return cfg
}
