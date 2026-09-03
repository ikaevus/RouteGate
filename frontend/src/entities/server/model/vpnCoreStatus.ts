export type VPNCoreState =
  | 'not_installed'
  | 'installed'
  | 'running'
  | 'stopped'
  | 'degraded'
  | 'failed'
  | 'unknown';

export interface VPNCoreStatus {
  type: string;
  installed: boolean;
  state: VPNCoreState;
  version: string | null;
  binaryPath: string | null;
  serviceName: string | null;
  serviceState: string | null;
  checkedAt: string | null;
  versionError: string | null;
  serviceError: string | null;
}

const knownStates = new Set<VPNCoreState>([
  'not_installed',
  'installed',
  'running',
  'stopped',
  'degraded',
  'failed',
  'unknown',
]);

const protocolRuntimeTypes: Record<string, string> = {
  vless: 'sing-box',
  shadowsocks: 'sing-box',
  wireguard: 'wireguard',
  hysteria2: 'hysteria',
  mtproto: 'mtg',
};

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function readString(value: unknown): string | null {
  return typeof value === 'string' && value.trim() !== '' ? value.trim() : null;
}

function readState(value: unknown): VPNCoreState {
  const candidate = readString(value);
  return candidate && knownStates.has(candidate as VPNCoreState)
    ? candidate as VPNCoreState
    : 'unknown';
}

export function resolveVPNCoreType(value?: string | null): string | null {
  const normalized = value?.trim().toLowerCase();
  if (!normalized) return null;
  return protocolRuntimeTypes[normalized] ?? normalized;
}

export function parseVPNCoreStatus(
  capabilities?: Record<string, unknown> | null,
  desiredType?: string | null,
): VPNCoreStatus | null {
  if (!capabilities) {
    return null;
  }

  const runtimeType = resolveVPNCoreType(desiredType);
  const cores = Array.isArray(capabilities.vpnCores) ? capabilities.vpnCores.filter(isRecord) : [];
  const selected = runtimeType
    ? cores.find((candidate) => readString(candidate.type)?.toLowerCase() === runtimeType)
    : undefined;
  const legacy = isRecord(capabilities.vpnCore) ? capabilities.vpnCore : undefined;

  // Legacy Agents reported only the original sing-box vpnCore object. Reusing
  // that object for a different requested runtime makes a healthy sing-box look
  // like healthy WireGuard/Hysteria/MTG and breaks multi-protocol onboarding.
  const raw = selected ?? (!runtimeType || runtimeType === 'sing-box' ? legacy : undefined);
  if (!raw) return null;

  const installed = raw.installed === true;
  const reportedState = readState(raw.state);
  const state = !installed && reportedState !== 'unknown'
    ? 'not_installed'
    : reportedState;

  return {
    type: readString(raw.type) ?? runtimeType ?? 'sing-box',
    installed,
    state,
    version: readString(raw.version),
    binaryPath: readString(raw.binaryPath),
    serviceName: readString(raw.serviceName),
    serviceState: readString(raw.serviceState),
    checkedAt: readString(raw.checkedAt),
    versionError: readString(raw.versionError),
    serviceError: readString(raw.serviceError),
  };
}

export function isVPNCoreOperational(status: VPNCoreStatus | null): boolean {
  return status?.installed === true && status.state === 'running';
}

export function needsVPNCoreAgentUpgrade(status: VPNCoreStatus | null): boolean {
  return status === null;
}
