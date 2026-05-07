package agents

import "time"

type Agent struct {
	ID        string    `json:"id"`
	ServerID  string    `json:"serverId"`
	Name      string    `json:"name"`
	Version   string    `json:"version"`
	Hostname  string    `json:"hostname"`
	Status    string    `json:"status"`
	LastSeen  time.Time `json:"lastSeen"`
	CreatedAt time.Time `json:"createdAt"`
}
