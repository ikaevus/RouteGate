package agents

import "context"

type telemetryHeartbeatWriter interface {
	UpsertAgentTelemetry(context.Context, string, string, HeartbeatTelemetry) error
}

func (r *Repository) UpsertAgentTelemetry(ctx context.Context, agentID, serverID string, telemetry HeartbeatTelemetry) error {
	return newTelemetryStore(r.pool).UpsertAgentTelemetry(ctx, agentID, serverID, telemetry)
}
