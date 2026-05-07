package servers

import "time"

type Server struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Hostname  string    `json:"hostname"`
	PublicIP  string    `json:"publicIp"`
	Location  string    `json:"location"`
	Provider  string    `json:"provider"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"createdAt"`
}
