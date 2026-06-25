DELETE FROM role_permissions rp
USING roles r, permissions p
WHERE rp.role_id = r.id
  AND rp.permission_id = p.id
  AND r.code IN ('super_admin', 'admin')
  AND p.code = 'traffic:manage';

DELETE FROM permissions
WHERE code = 'traffic:manage';

DROP TABLE IF EXISTS traffic_usage_events;
DROP TABLE IF EXISTS traffic_limits;
