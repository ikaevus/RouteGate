package routingprofiles

import "time"

const (
	ActionDirect = "direct"
	ActionVPN    = "vpn"
	ActionBlock  = "block"
)

type RoutingProfile struct {
	ID            string               `json:"id"`
	Name          string               `json:"name"`
	Description   string               `json:"description,omitempty"`
	IsDefault     bool                 `json:"isDefault"`
	DefaultAction string               `json:"defaultAction"`
	Rules         []RoutingProfileRule `json:"rules,omitempty"`
	CreatedAt     time.Time            `json:"createdAt"`
	UpdatedAt     time.Time            `json:"updatedAt"`
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

type ServerRoutingProfileAssignment struct {
	ServerID       string          `json:"serverId"`
	RoutingProfile *RoutingProfile `json:"routingProfile"`
	CreatedAt      *time.Time      `json:"createdAt,omitempty"`
	UpdatedAt      *time.Time      `json:"updatedAt,omitempty"`
}

type ListRoutingProfilesResponse struct {
	Items []RoutingProfile `json:"items"`
}

type CreateRoutingProfileRequest struct {
	Name          string `json:"name"`
	Description   string `json:"description"`
	IsDefault     bool   `json:"isDefault"`
	DefaultAction string `json:"defaultAction"`
}

type UpdateRoutingProfileRequest struct {
	Name          *string `json:"name,omitempty"`
	Description   *string `json:"description,omitempty"`
	IsDefault     *bool   `json:"isDefault,omitempty"`
	DefaultAction *string `json:"defaultAction,omitempty"`
}

type AssignServerRoutingProfileRequest struct {
	RoutingProfileID string `json:"routingProfileId"`
}

type CreateRoutingProfileInput struct {
	Name          string
	Description   string
	IsDefault     bool
	DefaultAction string
}

type UpdateRoutingProfileInput struct {
	Name          *string
	Description   *string
	IsDefault     *bool
	DefaultAction *string
}

type ManagedRuleSet struct {
	ID                   string     `json:"id"`
	RoutingProfileID     string     `json:"routingProfileId"`
	Name                 string     `json:"name"`
	Provider             string     `json:"provider"`
	SourceURL            string     `json:"sourceUrl"`
	SourceFormat         string     `json:"sourceFormat"`
	Priority             int        `json:"priority"`
	Action               string     `json:"action"`
	Enabled              bool       `json:"enabled"`
	RefreshIntervalHours int        `json:"refreshIntervalHours"`
	Status               string     `json:"status"`
	LastError            string     `json:"lastError,omitempty"`
	RuleCount            int        `json:"ruleCount"`
	LastRefreshAt        *time.Time `json:"lastRefreshAt,omitempty"`
	LastSuccessfulAt     *time.Time `json:"lastSuccessfulAt,omitempty"`
	CreatedAt            time.Time  `json:"createdAt"`
	UpdatedAt            time.Time  `json:"updatedAt"`
}

type CreateManagedRuleSetRequest struct {
	Name                 string `json:"name"`
	Provider             string `json:"provider"`
	SourceURL            string `json:"sourceUrl"`
	Priority             int    `json:"priority"`
	Action               string `json:"action"`
	Enabled              bool   `json:"enabled"`
	RefreshIntervalHours int    `json:"refreshIntervalHours"`
}

type UpdateManagedRuleSetRequest struct {
	Name                 *string `json:"name,omitempty"`
	Provider             *string `json:"provider,omitempty"`
	SourceURL            *string `json:"sourceUrl,omitempty"`
	Priority             *int    `json:"priority,omitempty"`
	Action               *string `json:"action,omitempty"`
	Enabled              *bool   `json:"enabled,omitempty"`
	RefreshIntervalHours *int    `json:"refreshIntervalHours,omitempty"`
}

type ListManagedRuleSetsResponse struct {
	Items []ManagedRuleSet `json:"items"`
}

type RoutingDiagnosticRequest struct {
	Target string `json:"target"`
}

type RoutingDiagnosticResponse struct {
	Target      string `json:"target"`
	ProfileID   string `json:"profileId"`
	ProfileName string `json:"profileName"`
	Action      string `json:"action"`
	Matched     bool   `json:"matched"`
	MatchedID   string `json:"matchedId,omitempty"`
	MatchedName string `json:"matchedName,omitempty"`
	Source      string `json:"source"`
	Priority    *int   `json:"priority,omitempty"`
	Precedence  string `json:"precedence"`
}

type ManagedRuleSetSnapshot struct {
	Version int                     `json:"version"`
	Rules   []ManagedRuleSetMatcher `json:"rules"`
}

type ManagedRuleSetMatcher struct {
	Domains        []string `json:"domain,omitempty"`
	DomainSuffixes []string `json:"domain_suffix,omitempty"`
	DomainKeywords []string `json:"domain_keyword,omitempty"`
	IPCIDRs        []string `json:"ip_cidr,omitempty"`
	GeoSites       []string `json:"geosite,omitempty"`
	GeoIPs         []string `json:"geoip,omitempty"`
	LogicalMode    string   `json:"type,omitempty"`
}

type AssignServerRoutingProfileInput struct {
	ServerID         string
	RoutingProfileID string
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
