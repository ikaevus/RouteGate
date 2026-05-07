package servers

type CreateServerRequest struct {
	Name     string `json:"name"`
	Hostname string `json:"hostname"`
	PublicIP string `json:"publicIp"`
	Location string `json:"location"`
	Provider string `json:"provider"`
}

type ListServersResponse struct {
	Items []Server `json:"items"`
}
