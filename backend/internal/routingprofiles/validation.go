package routingprofiles

import (
	"errors"
	"net/netip"
	"strings"
)

func validateCreateProfileInput(input CreateRoutingProfileInput) error {
	if err := validateProfileName(input.Name); err != nil {
		return err
	}
	return validateProfileDescription(input.Description)
}

func validateUpdateProfileInput(input UpdateRoutingProfileInput) error {
	if input.Name != nil {
		if err := validateProfileName(*input.Name); err != nil {
			return err
		}
	}
	if input.Description != nil {
		return validateProfileDescription(*input.Description)
	}
	return nil
}

func validateProfileName(name string) error {
	if name == "" {
		return errors.New("name is required")
	}
	if len([]rune(name)) > 120 {
		return errors.New("name must be 120 characters or fewer")
	}
	return nil
}

func validateProfileDescription(description string) error {
	if len([]rune(description)) > 1000 {
		return errors.New("description must be 1000 characters or fewer")
	}
	return nil
}

func validateCreateRuleInput(input CreateRoutingProfileRuleInput) error {
	if input.RoutingProfileID == "" {
		return errors.New("routing profile id is required")
	}
	if err := validateRuleName(input.Name); err != nil {
		return err
	}
	if !ValidAction(input.Action) {
		return errors.New("action must be one of: direct, vpn, block")
	}
	if err := validateRulePriority(input.Priority); err != nil {
		return err
	}
	if !hasRuleMatchers(input.Domains, input.DomainSuffixes, input.DomainKeywords, input.IPCIDRs, input.GeoSites, input.GeoIPs) {
		return errors.New("at least one rule matcher is required")
	}
	return validateRuleMatchers(input.Domains, input.DomainSuffixes, input.DomainKeywords, input.IPCIDRs, input.GeoSites, input.GeoIPs)
}

func validateUpdateRuleInput(input UpdateRoutingProfileRuleInput) error {
	if input.Name != nil {
		if err := validateRuleName(*input.Name); err != nil {
			return err
		}
	}
	if input.Action != nil && !ValidAction(*input.Action) {
		return errors.New("action must be one of: direct, vpn, block")
	}
	if input.Priority != nil {
		if err := validateRulePriority(*input.Priority); err != nil {
			return err
		}
	}
	if inputReplacesAllMatchers(input) && !hasRuleMatchers(
		stringSliceValue(input.Domains),
		stringSliceValue(input.DomainSuffixes),
		stringSliceValue(input.DomainKeywords),
		stringSliceValue(input.IPCIDRs),
		stringSliceValue(input.GeoSites),
		stringSliceValue(input.GeoIPs),
	) {
		return errors.New("at least one rule matcher is required")
	}
	return validateRuleMatchers(
		stringSliceValue(input.Domains),
		stringSliceValue(input.DomainSuffixes),
		stringSliceValue(input.DomainKeywords),
		stringSliceValue(input.IPCIDRs),
		stringSliceValue(input.GeoSites),
		stringSliceValue(input.GeoIPs),
	)
}

func validateRuleName(name string) error {
	if name == "" {
		return errors.New("name is required")
	}
	if len([]rune(name)) > 120 {
		return errors.New("name must be 120 characters or fewer")
	}
	return nil
}

func validateRulePriority(priority int) error {
	if priority < 0 {
		return errors.New("priority must be zero or greater")
	}
	if priority > 1000000 {
		return errors.New("priority must be 1000000 or less")
	}
	return nil
}

func inputReplacesAllMatchers(input UpdateRoutingProfileRuleInput) bool {
	return input.Domains != nil && input.DomainSuffixes != nil && input.DomainKeywords != nil && input.IPCIDRs != nil && input.GeoSites != nil && input.GeoIPs != nil
}

func hasRuleMatchers(groups ...[]string) bool {
	for _, group := range groups {
		if len(group) > 0 {
			return true
		}
	}
	return false
}

func validateRuleMatchers(domains, domainSuffixes, domainKeywords, ipCIDRs, geoSites, geoIPs []string) error {
	if err := validateDomainMatchers("domains", domains, false); err != nil {
		return err
	}
	if err := validateDomainMatchers("domainSuffixes", domainSuffixes, true); err != nil {
		return err
	}
	if err := validateLooseMatchers("domainKeywords", domainKeywords, 253); err != nil {
		return err
	}
	if err := validateIPCIDRs(ipCIDRs); err != nil {
		return err
	}
	if err := validateLooseMatchers("geoSites", geoSites, 100); err != nil {
		return err
	}
	return validateLooseMatchers("geoIps", geoIPs, 100)
}

func validateDomainMatchers(field string, values []string, allowLeadingDot bool) error {
	for _, value := range values {
		if !validDomainMatcher(value, allowLeadingDot) {
			return errors.New(field + " must contain valid domain values")
		}
	}
	return nil
}

func validDomainMatcher(value string, allowLeadingDot bool) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 253 {
		return false
	}
	if strings.Contains(value, "://") || strings.ContainsAny(value, " \t\r\n/:@\\*") || strings.Contains(value, "..") {
		return false
	}
	if allowLeadingDot {
		value = strings.TrimPrefix(value, ".")
	}
	if strings.HasPrefix(value, ".") || strings.HasSuffix(value, ".") {
		return false
	}
	labels := strings.Split(value, ".")
	if len(labels) < 2 {
		return false
	}
	for _, label := range labels {
		if !validDomainLabel(label) {
			return false
		}
	}
	return true
}

func validDomainLabel(label string) bool {
	if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
		return false
	}
	for _, r := range label {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' {
			continue
		}
		return false
	}
	return true
}

func validateLooseMatchers(field string, values []string, maxLength int) error {
	for _, value := range values {
		if strings.TrimSpace(value) == "" || len([]rune(value)) > maxLength || strings.ContainsAny(value, "\t\r\n") {
			return errors.New(field + " contains an invalid value")
		}
	}
	return nil
}

func validateIPCIDRs(values []string) error {
	for _, value := range values {
		if _, err := netip.ParsePrefix(value); err != nil {
			return errors.New("ipCidrs must contain valid CIDR prefixes")
		}
	}
	return nil
}

func cleanStrings(values []string) []string {
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		cleaned = append(cleaned, value)
	}
	return cleaned
}

func cleanStringSlicePointer(values *[]string) {
	if values == nil {
		return
	}
	cleaned := cleanStrings(*values)
	*values = cleaned
}

func trimStringPointer(value *string) {
	if value != nil {
		*value = strings.TrimSpace(*value)
	}
}
