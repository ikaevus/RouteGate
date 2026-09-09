export type ClientCompatibilityStatus =
  | 'full_smart_routing'
  | 'client_setup_required'
  | 'partial_compatibility'
  | 'connection_only';

export interface ClientCapabilities {
  fullSingBoxConfigImport: boolean;
  uriSubscriptionImport: boolean;
  tunMode: boolean;
  directRouting: boolean;
  vpnRouting: boolean;
  blockRouting: boolean;
  remoteRuleSets: boolean;
  dnsRouting: boolean;
  splitDns: boolean;
  clientLocalRules: boolean;
  subscriptionRefresh: boolean;
  subscriptionRoutingPolicy: boolean;
  importedRulePrecedence: string;
}

export interface ClientCompatibilityAssessment {
  clientType: string;
  displayName: string;
  status: ClientCompatibilityStatus;
  preferredDeliveryFormat: string;
  requiresClientSetup: boolean;
  capabilities: ClientCapabilities;
  guidance?: string[];
  limitations?: string[];
}

type CompatibilityCarrier = {
  clientCompatibility?: ClientCompatibilityAssessment;
};

export function getClientCompatibility(value: unknown): ClientCompatibilityAssessment | undefined {
  if (!value || typeof value !== 'object') return undefined;
  return (value as CompatibilityCarrier).clientCompatibility;
}
