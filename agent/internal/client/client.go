package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/artuazh/routegate/agent/internal/config"
	"github.com/artuazh/routegate/agent/internal/systeminfo"
)

const timeout = 10 * time.Second

type Client struct {
	managerURL string
	httpClient *http.Client
}

func New(managerURL string) *Client {
	return &Client{managerURL: strings.TrimRight(managerURL, "/"), httpClient: &http.Client{Timeout: timeout}}
}

type registerRequest struct {
	RegistrationToken string         `json:"registrationToken"`
	Hostname          string         `json:"hostname"`
	AgentVersion      string         `json:"agentVersion"`
	OS                string         `json:"os"`
	Arch              string         `json:"arch"`
	Capabilities      map[string]any `json:"capabilities"`
}

type RegisterResponse struct {
	AgentID    string `json:"agentId"`
	ServerID   string `json:"serverId"`
	AgentToken string `json:"agentToken"`
}

type heartbeatRequest struct {
	AgentVersion string         `json:"agentVersion"`
	Capabilities map[string]any `json:"capabilities"`
}

type HeartbeatResponse struct {
	OK           bool   `json:"ok"`
	AgentID      string `json:"agentId"`
	ServerID     string `json:"serverId"`
	ServerStatus string `json:"serverStatus"`
}

func (c *Client) Register(ctx context.Context, cfg config.Config, info systeminfo.Info) (RegisterResponse, error) {
	req := registerRequest{RegistrationToken: cfg.RegistrationToken, Hostname: info.Hostname, AgentVersion: info.AgentVersion, OS: info.OS, Arch: info.Arch, Capabilities: info.Capabilities}
	var res RegisterResponse
	if err := c.postJSON(ctx, "/api/v1/agent/register", "", req, &res); err != nil {
		return RegisterResponse{}, err
	}
	if res.AgentID == "" || res.ServerID == "" || res.AgentToken == "" {
		return RegisterResponse{}, fmt.Errorf("registration response missing credentials")
	}
	return res, nil
}

func (c *Client) Heartbeat(ctx context.Context, agentToken string, info systeminfo.Info) (HeartbeatResponse, error) {
	req := heartbeatRequest{AgentVersion: info.AgentVersion, Capabilities: info.Capabilities}
	var res HeartbeatResponse
	if err := c.postJSON(ctx, "/api/v1/agent/heartbeat", agentToken, req, &res); err != nil {
		return HeartbeatResponse{}, err
	}
	return res, nil
}

func (c *Client) postJSON(ctx context.Context, path, bearer string, body any, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.managerURL+path, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("POST %s failed with status %d: %s", path, resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	if out != nil && len(respBody) > 0 {
		if err := json.Unmarshal(respBody, out); err != nil {
			return err
		}
	}
	return nil
}
