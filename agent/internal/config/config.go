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
const DefaultActiveConfigPath = "/etc/sing-box/config.json"
const DefaultConfigBackupDir = "/var/lib/routegate-agent/backups"
const DefaultSingBoxPath = "sing-box"
const DefaultSingBoxServiceName = "sing-box"
const DefaultServiceControlEnabled = true
const DefaultTrafficCollectionIntervalSeconds = 60
const DefaultTrafficUsageFilePath = "/var/lib/routegate-agent/traffic-usage.json"
const DefaultWireGuardStagingDir = "/var/lib/routegate-agent/wireguard-configs"
const DefaultWireGuardActiveConfigPath = "/etc/wireguard/routegate-wg0.conf"
const DefaultWireGuardBackupDir = "/var/lib/routegate-agent/wireguard-backups"
const DefaultWGQuickPath = "wg-quick"
const DefaultWGPath = "wg"
const DefaultWireGuardServiceName = "wg-quick@routegate-wg0"
const DefaultWireGuardInterface = "routegate-wg0"

type Config struct {
	ManagerURL                       string
	RegistrationToken                string
	AgentID                          string
	ServerID                         string
	AgentToken                       string
	HeartbeatIntervalSeconds         int
	ConfigStagingDir                 string
	ActiveConfigPath                 string
	ConfigBackupDir                  string
	SingBoxPath                      string
	SingBoxServiceName               string
	ServiceControlEnabled            bool
	TrafficCollectionEnabled         bool
	TrafficCollectionIntervalSeconds int
	TrafficUsageFilePath             string
	WireGuardStagingDir              string
	WireGuardActiveConfigPath        string
	WireGuardBackupDir               string
	WGQuickPath                      string
	WGPath                           string
	WireGuardServiceName             string
	WireGuardInterface               string
}

func Load(path string) (Config, error) {
	file, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}
	defer file.Close()
	cfg := Config{ServiceControlEnabled: DefaultServiceControlEnabled}
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
		case "active_config_path":
			cfg.ActiveConfigPath = value
		case "config_backup_dir":
			cfg.ConfigBackupDir = value
		case "sing_box_path":
			cfg.SingBoxPath = value
		case "sing_box_service_name":
			cfg.SingBoxServiceName = value
		case "service_control_enabled":
			if value == "" {
				continue
			}
			parsed, err := strconv.ParseBool(value)
			if err != nil {
				return Config{}, fmt.Errorf("parse service_control_enabled: %w", err)
			}
			cfg.ServiceControlEnabled = parsed
		case "traffic_collection_enabled":
			if value == "" {
				continue
			}
			parsed, err := strconv.ParseBool(value)
			if err != nil {
				return Config{}, fmt.Errorf("parse traffic_collection_enabled: %w", err)
			}
			cfg.TrafficCollectionEnabled = parsed
		case "traffic_collection_interval_seconds":
			if value == "" {
				continue
			}
			parsed, err := strconv.Atoi(value)
			if err != nil {
				return Config{}, fmt.Errorf("parse traffic_collection_interval_seconds: %w", err)
			}
			cfg.TrafficCollectionIntervalSeconds = parsed
		case "traffic_usage_file_path":
			cfg.TrafficUsageFilePath = value
		case "wireguard_staging_dir":
			cfg.WireGuardStagingDir = value
		case "wireguard_active_config_path":
			cfg.WireGuardActiveConfigPath = value
		case "wireguard_backup_dir":
			cfg.WireGuardBackupDir = value
		case "wg_quick_path":
			cfg.WGQuickPath = value
		case "wg_path":
			cfg.WGPath = value
		case "wireguard_service_name":
			cfg.WireGuardServiceName = value
		case "wireguard_interface":
			cfg.WireGuardInterface = value
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
	cfg.ActiveConfigPath = strings.TrimSpace(cfg.ActiveConfigPath)
	cfg.ConfigBackupDir = strings.TrimSpace(cfg.ConfigBackupDir)
	cfg.SingBoxPath = strings.TrimSpace(cfg.SingBoxPath)
	cfg.SingBoxServiceName = strings.TrimSpace(cfg.SingBoxServiceName)
	cfg.TrafficUsageFilePath = strings.TrimSpace(cfg.TrafficUsageFilePath)
	cfg.WireGuardStagingDir = strings.TrimSpace(cfg.WireGuardStagingDir)
	cfg.WireGuardActiveConfigPath = strings.TrimSpace(cfg.WireGuardActiveConfigPath)
	cfg.WireGuardBackupDir = strings.TrimSpace(cfg.WireGuardBackupDir)
	cfg.WGQuickPath = strings.TrimSpace(cfg.WGQuickPath)
	cfg.WGPath = strings.TrimSpace(cfg.WGPath)
	cfg.WireGuardServiceName = strings.TrimSpace(cfg.WireGuardServiceName)
	cfg.WireGuardInterface = strings.TrimSpace(cfg.WireGuardInterface)
	if cfg.HeartbeatIntervalSeconds <= 0 {
		cfg.HeartbeatIntervalSeconds = 30
	}
	if cfg.ConfigStagingDir == "" {
		cfg.ConfigStagingDir = DefaultConfigStagingDir
	}
	if cfg.ActiveConfigPath == "" {
		cfg.ActiveConfigPath = DefaultActiveConfigPath
	}
	if cfg.ConfigBackupDir == "" {
		cfg.ConfigBackupDir = DefaultConfigBackupDir
	}
	if cfg.SingBoxPath == "" {
		cfg.SingBoxPath = DefaultSingBoxPath
	}
	if cfg.SingBoxServiceName == "" {
		cfg.SingBoxServiceName = DefaultSingBoxServiceName
	}
	if cfg.TrafficCollectionIntervalSeconds <= 0 {
		cfg.TrafficCollectionIntervalSeconds = DefaultTrafficCollectionIntervalSeconds
	}
	if cfg.TrafficUsageFilePath == "" {
		cfg.TrafficUsageFilePath = DefaultTrafficUsageFilePath
	}
	if cfg.WireGuardStagingDir == "" { cfg.WireGuardStagingDir = DefaultWireGuardStagingDir }
	if cfg.WireGuardActiveConfigPath == "" { cfg.WireGuardActiveConfigPath = DefaultWireGuardActiveConfigPath }
	if cfg.WireGuardBackupDir == "" { cfg.WireGuardBackupDir = DefaultWireGuardBackupDir }
	if cfg.WGQuickPath == "" { cfg.WGQuickPath = DefaultWGQuickPath }
	if cfg.WGPath == "" { cfg.WGPath = DefaultWGPath }
	if cfg.WireGuardServiceName == "" { cfg.WireGuardServiceName = DefaultWireGuardServiceName }
	if cfg.WireGuardInterface == "" { cfg.WireGuardInterface = DefaultWireGuardInterface }
	if cfg.ManagerURL == "" {
		return Config{}, errors.New("manager_url is required")
	}
	return cfg, nil
}

func (c Config) HeartbeatInterval() time.Duration {
	return time.Duration(c.HeartbeatIntervalSeconds) * time.Second
}

func (c Config) TrafficCollectionInterval() time.Duration {
	return time.Duration(c.TrafficCollectionIntervalSeconds) * time.Second
}

func (c Config) Save(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	stagingDir := c.ConfigStagingDir
	if stagingDir == "" {
		stagingDir = DefaultConfigStagingDir
	}
	activeConfigPath := c.ActiveConfigPath
	if activeConfigPath == "" {
		activeConfigPath = DefaultActiveConfigPath
	}
	backupDir := c.ConfigBackupDir
	if backupDir == "" {
		backupDir = DefaultConfigBackupDir
	}
	singBoxPath := c.SingBoxPath
	if singBoxPath == "" {
		singBoxPath = DefaultSingBoxPath
	}
	serviceName := c.SingBoxServiceName
	if serviceName == "" {
		serviceName = DefaultSingBoxServiceName
	}
	serviceControlEnabled := c.ServiceControlEnabled
	trafficInterval := c.TrafficCollectionIntervalSeconds
	if trafficInterval <= 0 {
		trafficInterval = DefaultTrafficCollectionIntervalSeconds
	}
	trafficUsageFilePath := c.TrafficUsageFilePath
	if trafficUsageFilePath == "" {
		trafficUsageFilePath = DefaultTrafficUsageFilePath
	}
	wireGuardStagingDir := defaultString(c.WireGuardStagingDir, DefaultWireGuardStagingDir)
	wireGuardActiveConfigPath := defaultString(c.WireGuardActiveConfigPath, DefaultWireGuardActiveConfigPath)
	wireGuardBackupDir := defaultString(c.WireGuardBackupDir, DefaultWireGuardBackupDir)
	wgQuickPath := defaultString(c.WGQuickPath, DefaultWGQuickPath)
	wgPath := defaultString(c.WGPath, DefaultWGPath)
	wireGuardServiceName := defaultString(c.WireGuardServiceName, DefaultWireGuardServiceName)
	wireGuardInterface := defaultString(c.WireGuardInterface, DefaultWireGuardInterface)

	var output strings.Builder
	fmt.Fprintf(&output, "manager_url: %q\n", c.ManagerURL)
	if c.RegistrationToken != "" {
		fmt.Fprintf(&output, "registration_token: %q\n", c.RegistrationToken)
	}
	fmt.Fprintf(&output, "agent_id: %q\nserver_id: %q\nagent_token: %q\n", c.AgentID, c.ServerID, c.AgentToken)
	fmt.Fprintf(&output, "heartbeat_interval_seconds: %d\n", c.HeartbeatIntervalSeconds)
	fmt.Fprintf(&output, "config_staging_dir: %q\nactive_config_path: %q\nconfig_backup_dir: %q\n", stagingDir, activeConfigPath, backupDir)
	fmt.Fprintf(&output, "sing_box_path: %q\nsing_box_service_name: %q\n", singBoxPath, serviceName)
	fmt.Fprintf(&output, "wireguard_staging_dir: %q\nwireguard_active_config_path: %q\nwireguard_backup_dir: %q\n", wireGuardStagingDir, wireGuardActiveConfigPath, wireGuardBackupDir)
	fmt.Fprintf(&output, "wg_quick_path: %q\nwg_path: %q\nwireguard_service_name: %q\nwireguard_interface: %q\n", wgQuickPath, wgPath, wireGuardServiceName, wireGuardInterface)
	fmt.Fprintf(&output, "service_control_enabled: %t\ntraffic_collection_enabled: %t\ntraffic_collection_interval_seconds: %d\ntraffic_usage_file_path: %q\n", serviceControlEnabled, c.TrafficCollectionEnabled, trafficInterval, trafficUsageFilePath)
	data := output.String()

	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
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
