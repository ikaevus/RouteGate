# Manager Agent Trust Notes

This note records the first baseline for the boundary between Manager and Agent API routes.

Admin API credentials and Agent API credentials are separate. Agent-only operations must use Agent credentials. Current Agent-only operations are heartbeat, task polling, and task completion.

The first implementation rule is simple: Agent-only routes require values with the Agent credential prefix before repository lookup. This keeps user sessions and Agent credentials from being mixed accidentally.

Current audit events added for the task flow are agent task claimed, agent task completed, and agent task completion rejected.

Future improvements can add credential rotation, signed task payloads, and stronger replay protection.
