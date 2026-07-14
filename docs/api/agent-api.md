# Agent API Draft

Agent runtime endpoints:

```text
GET /api/agent/health
POST /api/v1/agent/register
POST /api/v1/agent/heartbeat
GET  /api/v1/agent/tasks/next
POST /api/v1/agent/tasks/{job_id}/result
POST /api/v1/agent/traffic-usage
```

Registration and heartbeat include Agent build and protocol metadata:

```json
{
  "agentVersion": "dev",
  "protocolVersion": 1
}
```

Manager stores the most recently reported values and uses `protocolVersion` as the compatibility boundary.
