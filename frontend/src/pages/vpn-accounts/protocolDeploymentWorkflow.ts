import { apiGet } from '../../shared/api/client';
import {
  applyConfigVersion,
  renderConfig,
  validateConfigVersion,
  type ConfigApplyJob,
  type Server,
} from '../../entities/server/api/serverApi';
import {
  createVPNCoreInstallation,
  getVPNCoreInstallation,
  type VPNRuntimeInstallationOperation,
} from '../../entities/server/api/vpnCoreApi';
import type { ClientProtocolPreference } from '../../entities/vpnAccount/api/vpnAccountApi';

const POLL_INTERVAL_MS = 1200;
const INSTALL_TIMEOUT_MS = 5 * 60 * 1000;
const APPLY_TIMEOUT_MS = 3 * 60 * 1000;

type ConcreteProtocol = Exclude<ClientProtocolPreference, 'auto'>;

type RuntimeRequirement = {
  coreType: string;
  installOperation: VPNRuntimeInstallationOperation;
  reconcileWhenInstalled?: boolean;
};

const runtimeRequirements: Record<ConcreteProtocol, RuntimeRequirement> = {
  vless: { coreType: 'sing-box', installOperation: 'install_sing_box' },
  shadowsocks: { coreType: 'sing-box', installOperation: 'install_sing_box' },
  wireguard: {
    coreType: 'wireguard',
    installOperation: 'install_wireguard',
    reconcileWhenInstalled: true,
  },
  hysteria2: { coreType: 'hysteria', installOperation: 'install_hysteria2' },
  mtproto: { coreType: 'mtg', installOperation: 'install_mtg' },
};

export type ProtocolDeploymentStage =
  | 'saving_preference'
  | 'checking_runtime'
  | 'installing_runtime'
  | 'rendering_config'
  | 'validating_config'
  | 'applying_config'
  | 'waiting_for_apply'
  | 'completed';

export class ProtocolDeploymentError extends Error {
  constructor(
    public readonly code: string,
    message: string,
  ) {
    super(message);
    this.name = 'ProtocolDeploymentError';
  }
}

function asRecord(value: unknown): Record<string, unknown> | null {
  return value && typeof value === 'object' && !Array.isArray(value)
    ? value as Record<string, unknown>
    : null;
}

function stringArray(value: unknown): string[] {
  return Array.isArray(value) ? value.filter((item): item is string => typeof item === 'string') : [];
}

function runtimeInstalled(server: Server, coreType: string): boolean {
  const capabilities = server.agent?.capabilities;
  if (!capabilities) {
    return false;
  }
  const vpnCores = Array.isArray(capabilities.vpnCores) ? capabilities.vpnCores : [];
  return vpnCores.some((candidate) => {
    const core = asRecord(candidate);
    return core?.type === coreType && core.installed === true;
  });
}

function advertisedInstallOperations(server: Server): string[] {
  return stringArray(server.agent?.capabilities?.vpnCoreInstallationOperations);
}

function delay(ms: number): Promise<void> {
  return new Promise((resolve) => window.setTimeout(resolve, ms));
}

async function waitForInstallation(serverId: string, jobId: string): Promise<void> {
  const deadline = Date.now() + INSTALL_TIMEOUT_MS;
  while (Date.now() < deadline) {
    const job = await getVPNCoreInstallation(serverId, jobId);
    if (job.status === 'succeeded') {
      return;
    }
    if (job.status === 'failed') {
      throw new ProtocolDeploymentError(
        job.errorMessage || 'runtime_installation_failed',
        'VPN runtime installation failed.',
      );
    }
    await delay(POLL_INTERVAL_MS);
  }
  throw new ProtocolDeploymentError('runtime_installation_timeout', 'VPN runtime installation timed out.');
}

async function getConfigApplyJob(serverId: string, jobId: string): Promise<ConfigApplyJob> {
  return apiGet<ConfigApplyJob>(
    `/api/v1/servers/${encodeURIComponent(serverId)}/config/apply-jobs/${encodeURIComponent(jobId)}`,
  );
}

async function waitForApply(serverId: string, jobId: string): Promise<void> {
  const deadline = Date.now() + APPLY_TIMEOUT_MS;
  while (Date.now() < deadline) {
    const job = await getConfigApplyJob(serverId, jobId);
    if (job.status === 'succeeded') {
      return;
    }
    if (job.status === 'failed') {
      throw new ProtocolDeploymentError(
        job.errorMessage || 'config_apply_failed',
        'VPN configuration deployment failed.',
      );
    }
    await delay(POLL_INTERVAL_MS);
  }
  throw new ProtocolDeploymentError('config_apply_timeout', 'VPN configuration deployment timed out.');
}

export async function ensureProtocolRuntime(
  server: Server,
  protocol: ClientProtocolPreference,
  onStage: (stage: ProtocolDeploymentStage) => void,
): Promise<void> {
  if (protocol === 'auto') {
    return;
  }
  const requirement = runtimeRequirements[protocol];
  onStage('checking_runtime');
  const installed = runtimeInstalled(server, requirement.coreType);
  if (installed && !requirement.reconcileWhenInstalled) {
    return;
  }
  if (!server.agent) {
    throw new ProtocolDeploymentError('agent_missing', 'The assigned VPN node has no connected Agent.');
  }
  if (!advertisedInstallOperations(server).includes(requirement.installOperation)) {
    throw new ProtocolDeploymentError(
      'runtime_installation_unsupported',
      'The assigned Agent cannot install the runtime required by this protocol.',
    );
  }

  // An installed runtime is not always activation-ready. In particular,
  // historical WireGuard nodes can have wg/wg-quick present while the managed
  // wg-quick instance is still disabled. Re-running the idempotent installer
  // reconciles service persistence before the transactional config apply.
  onStage('installing_runtime');
  const response = await createVPNCoreInstallation(server.id, requirement.installOperation);
  await waitForInstallation(server.id, response.job.id);
}

export async function deployPendingProtocol(
  serverId: string,
  onStage: (stage: ProtocolDeploymentStage) => void,
): Promise<void> {
  onStage('rendering_config');
  const rendered = await renderConfig(serverId);
  if (!rendered.validationResult.valid) {
    throw new ProtocolDeploymentError(
      'rendered_config_invalid',
      rendered.validationResult.errors[0] || 'Rendered VPN configuration is invalid.',
    );
  }

  onStage('validating_config');
  const validated = await validateConfigVersion(serverId, rendered.configVersion.id);
  if (!validated.validationResult.valid) {
    throw new ProtocolDeploymentError(
      'config_validation_failed',
      validated.validationResult.errors[0] || 'VPN configuration validation failed.',
    );
  }

  onStage('applying_config');
  const applied = await applyConfigVersion(serverId, rendered.configVersion.id);
  onStage('waiting_for_apply');
  await waitForApply(serverId, applied.job.id);
  onStage('completed');
}
