# Agent API

RouteGate Agent uses the versioned Agent-to-Manager API only:

```text
POST /api/v1/agent/register
POST /api/v1/agent/heartbeat
GET  /api/v1/agent/tasks/next
POST /api/v1/agent/tasks/{job_id}/result
POST /api/v1/agent/traffic-usage
```

Administrators read registered Agents through the authenticated Manager API:

```text
GET /api/v1/agents
```

Registration uses a short-lived server registration token. A successful registration returns a persistent Agent credential, which is then used for heartbeat, task polling, task completion, and traffic reporting.

Registration and heartbeat include Agent build and protocol metadata:

```json
{
  "agentVersion": "dev",
  "protocolVersion": 1
}
```

Manager stores the most recently reported values and uses `protocolVersion` as the compatibility boundary.

The old unversioned `/api/agent/*` compatibility endpoints and manual Agent registration UI are not part of the supported RouteGate runtime.
