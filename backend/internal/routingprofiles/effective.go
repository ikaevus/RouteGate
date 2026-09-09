package routingprofiles

import "context"

// GetEffectiveProfile preserves account override -> server assignment -> global
// default. Both subscriptions and account diagnostics use this exact query.
func (r *Repository) GetEffectiveProfile(ctx context.Context, accountID, serverID string) (RoutingProfile, error) {
	var id string
	err := r.pool.QueryRow(ctx, `SELECT id::text FROM routing_profiles WHERE id=COALESCE(
 (SELECT routing_profile_id FROM vpn_account_routing_profiles WHERE vpn_account_id=NULLIF($1,'')::uuid),
 (SELECT routing_profile_id FROM server_routing_profiles WHERE server_id=NULLIF($2,'')::uuid),
 (SELECT id FROM routing_profiles WHERE is_default=TRUE ORDER BY created_at,id LIMIT 1)
 )`, accountID, serverID).Scan(&id)
	if err != nil {
		return RoutingProfile{}, err
	}
	return r.GetProfile(ctx, id)
}
