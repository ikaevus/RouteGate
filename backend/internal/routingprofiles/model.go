package routingprofiles

import "time"

const (
	ActionDirect = "direct"
	ActionVPN    = "vpn"
	ActionBlock  = "block"
)

type RoutingProfile struct {
	ID          string               `json:"id"`
	Name        string               `json:"name"`
	Description string               `json:"description,omitempty"`
	IsDefault   bool                 `json:"isDefault"`
	Rules       []RoutingProfileRule `json:"rules,omitempty"`
	CreatedAt   time.Time            `json:"createdAt"`
	UpdatedAt   time.Time            `json:"updatedAt"`
}

type RoutingProfileRule struct {
	ID               string    `json:"id"`
	RoutingProfileID string    `json:"routingProfileId"`
	Name             string    `json:"name"`
	Priority         int       `json:"priority"`
	Action           string    `json:"action"`
	Domains          []string  `json:"domains,omitempty"`
	DomainSuffixes   []string  `json:"domainSuffixes,omitempty"`
	DomainKeywords   []string  `json:"domainKeywords,omitempty"`
	IPCIDRs          []string  `json:"ipCidrs,omitempty"`
	GeoSites         []string  `json:"geoSites,omitempty"`
	GeoIPs           []string  `json:"geoIps,omitempty"`
	Enabled          bool      `json:"enabled"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

type ListRoutingProfilesResponse struct {
	Items []RoutingProfile `json:"items"`
}

type CreateRoutingProfileRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	IsDefault   bool   `json:"isDefault"`
}

type UpdateRoutingProfileRequest struct {
	Name        *string `json:"name,omitempty"`
	Description *string `json:"description,omitempty"`
	IsDefault   *bool   `json:"isDefault,omitempty"`
}

type CreateRoutingProfileInput struct {
	Name        string
	Description string
	IsDefault   bool
}

type UpdateRoutingProfileInput struct {
	Name        *string
	Description *string
	IsDefault   *bool
}

type CreateRoutingProfileRuleRequest struct {
	Name           string   `json:"name"`
	Priority       int      `json:"priority"`
	Action         string   `json:"action"`
	Domains        []string `json:"domains"`
	DomainSuffixes []string `json:"domainSuffixes"`
	DomainKeywords []string `json:"domainKeywords"`
	IPCIDRs        []string `json:"ipCidrs"`
	GeoSites       []string `json:"geoSites"`
	GeoIPs         []string `json:"geoIps"`
	Enabled        *bool    `json:"enabled,omitempty"`
}

type UpdateRoutingProfileRuleRequest struct {
	Name           *string   `json:"name,omitempty"`
	Priority       *int      `json:"priority,omitempty"`
	Action         *string   `json:"action,omitempty"`
	Domains        *[]string `json:"domains,omitempty"`
	DomainSuffixes *[]string `json:"domainSuffixes,omitempty"`
	DomainKeywords *[]string `json:"domainKeywords,omitempty"`
	IPCIDRs        *[]string `json:"ipCidrs,omitempty"`
	GeoSites       *[]string `json:"geoSites,omitempty"`
	GeoIPs         *[]string `json:"geoIps,omitempty"`
	Enabled        *bool     `json:"enabled,omitempty"`
}

type CreateRoutingProfileRuleInput struct {
	RoutingProfileID string
	Name             string
	Priority         int
	Action           string
	Domains          []string
	DomainSuffixes   []string
	DomainKeywords   []string
	IPCIDRs          []string
	GeoSites         []string
	GeoIPs           []string
	Enabled          bool
}

type UpdateRoutingProfileRuleInput struct {
	Name           *string
	Priority       *int
	Action         *string
	Domains        *[]string
	DomainSuffixes *[]string
	DomainKeywords *[]string
	IPCIDRs        *[]string
	GeoSites       *[]string
	GeoIPs         *[]string
	Enabled        *bool
}

func ValidAction(action string) bool {
	switch action {
	case ActionDirect, ActionVPN, ActionBlock:
		return true
	default:
		return false
	}
}
