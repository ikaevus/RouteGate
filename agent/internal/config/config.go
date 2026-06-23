package config

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const DefaultPath = "/etc/routegate/agent.yaml"
const DefaultConfigStagingDir = "/var/lib/routegate-agent/configs"

type Config struct {
	ManagerURL               string
	RegistrationToken        string
	AgentID                  string
	ServerID                 string
	AgentToken               string
	HeartbeatIntervalSeconds int
	ConfigStagingDir         string
}

func Load(path string) (Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	defer file.Close()
	cfg := Config{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := stripComment(scanner.Text())
		if strings.TrimSpace(line) == "" {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			return Config{}, fmt.Errorf("parse config: invalid line %q", scanner.Text())
		}
		key = strings.TrimSpace(key)
		value = trimYAMLScalar(value)
		switch key {
		case "manager_url":
			cfg.ManagerURL = value
		case "registration_token":
			cfg.RegistrationToken = value
		case "agent_id":
			cfg.AgentID = value
		case "server_id":
			cfg.ServerID = value
		case "agent_token":
			cfg.AgentToken = value
		case "heartbeat_interval_seconds":
			if value == "" {
				continue
			}
			parsed, err := strconv.Atoi(value)
			if err != nil {
				return Config{}, fmt.Errorf("parse heartbeat_interval_seconds: %w", err)
			}
			cfg.HeartbeatIntervalSeconds = parsed
		case "config_staging_dir":
			cfg.ConfigStagingDir = value
		}
	}
	if err := scanner.Err(); err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	cfg.ManagerURL = strings.TrimRight(strings.TrimSpace(cfg.ManagerURL), "/")
	cfg.RegistrationToken = strings.TrimSpace(cfg.RegistrationToken)
	cfg.AgentID = strings.TrimSpace(cfg.AgentID)
	cfg.ServerID = strings.TrimSpace(cfg.ServerID)
	cfg.AgentToken = strings.TrimSpace(cfg.AgentToken)
	cfg.ConfigStagingDir = strings.TrimSpace(cfg.ConfigStagingDir)
	if cfg.HeartbeatIntervalSeconds <= 0 {
		cfg.HeartbeatIntervalSeconds = 30
	}
	if cfg.ConfigStagingDir == "" {
		cfg.ConfigStagingDir = DefaultConfigStagingDir
	}
	if cfg.ManagerURL == "" {
		return Config{}, errors.New("manager_url is required")
	}
	return cfg, nil
}

func (c Config) HeartbeatInterval() time.Duration {
	return time.Duration(c.HeartbeatIntervalSeconds) * time.Second
}

func (c Config) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	stagingDir := c.ConfigStagingDir
	if stagingDir == "" {
		stagingDir = DefaultConfigStagingDir
	}
	data := fmt.Sprintf("manager_url: %q\nagent_id: %q\nserver_id: %q\nagent_token: %q\nheartbeat_interval_seconds: %d\nconfig_staging_dir: %q\n", c.ManagerURL, c.AgentID, c.ServerID, c.AgentToken, c.HeartbeatIntervalSeconds, stagingDir)
	if c.RegistrationToken != "" {
		data = fmt.Sprintf("manager_url: %q\nregistration_token: %q\nagent_id: %q\nserver_id: %q\nagent_token: %q\nheartbeat_interval_seconds: %d\nconfig_staging_dir: %q\n", c.ManagerURL, c.RegistrationToken, c.AgentID, c.ServerID, c.AgentToken, c.HeartbeatIntervalSeconds, stagingDir)
	}
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

func (c Config) HasAgentCredentials() bool  { return c.AgentToken != "" }
func (c Config) HasRegistrationToken() bool { return c.RegistrationToken != "" }

func stripComment(line string) string {
	inSingle, inDouble := false, false
	for i, r := range line {
		switch r {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case '#':
			if !inSingle && !inDouble {
				return line[:i]
			}
		}
	}
	return line
}

func trimYAMLScalar(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 2 {
		if unquoted, err := strconv.Unquote(value); err == nil {
			return unquoted
		}
		if value[0] == '\'' && value[len(value)-1] == '\'' {
			return strings.ReplaceAll(value[1:len(value)-1], "''", "'")
		}
	}
	return value
}
