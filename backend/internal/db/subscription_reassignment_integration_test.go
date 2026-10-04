package db

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ikaevus/routegate/backend/internal/configs"
	"github.com/ikaevus/routegate/backend/internal/vpnaccounts"
)

// RG-140: link identity is account/device scoped, not bound to server role,
// name, endpoint, applied version, or transfer direction. This tests the
// EXISTING manual-reassignment path, including its unsafe interruption window;
// it does not assert seamless migration or an automatic client refresh.
func TestDeviceSubscriptionSurvivesBidirectionalHybridVPNNodeReassignment(t *testing.T) {
	for _, tc := range []struct {
		name       string
		sourceRole string
		targetRole string
		sourcePort int
		targetPort int
	}{
		{"hybrid_to_vpn_and_back", "hybrid", "vpn", 8443, 443},
		{"vpn_to_hybrid_and_back", "vpn", "hybrid", 443, 8443},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, pool, done := setupAppliedSettingsTest(t)
			defer done()

			sourceKey, targetKey := newRealityTestKey(t), newRealityTestKey(t)
			sourceID, accountID, _ := createAppliedSettingsServer(t, ctx, pool, sourceKey)
			targetID := createReassignmentTarget(t, ctx, pool, targetKey)
			setReassignmentNodeTopology(t, ctx, pool, sourceID, tc.sourceRole, tc.sourcePort)
			setReassignmentNodeTopology(t, ctx, pool, targetID, tc.targetRole, tc.targetPort)
			accounts := vpnaccounts.NewRepository(pool)
			render := configs.NewService(configs.NewRepository(pool))

			sourceV1 := renderVersion(t, ctx, render, sourceID)
			finishApply(t, ctx, pool, sourceID, sourceV1, "succeeded")

			device, err := accounts.CreateDevice(ctx, vpnaccounts.CreateDeviceInput{
				VPNAccountID: accountID, Name: "Android test",
				ClientType: vpnaccounts.ClientTypeHiddify, DeviceType: vpnaccounts.DevicePlatformAndroid,
			})
			if err != nil {
				t.Fatalf("create device: %v", err)
			}
			rawToken := "rg-140-stable-device-token"
			originalToken, err := accounts.CreateDeviceSubscriptionToken(ctx, device.ID, vpnaccounts.HashSubscriptionToken(rawToken), nil)
			if err != nil {
				t.Fatalf("create device subscription token: %v", err)
			}
			probe := newSubscriptionProbe(t, ctx, pool, accounts)
			fetch := func() (int, string) {
				request := httptest.NewRequest(http.MethodGet, "/sub/"+rawToken+"?format=raw", nil)
				request.SetPathValue("token", rawToken)
				response := httptest.NewRecorder()
				probe.handler.GetClientSubscription(response, request)
				return response.Code, response.Body.String()
			}
			assertServed := func(host string, port int, key realityTestKey) {
				t.Helper()
				status, body := fetch()
				if status != http.StatusOK || !strings.Contains(body, host+":"+strconv.Itoa(port)) ||
					!strings.Contains(body, key.public) {
					t.Fatalf("stable device subscription must serve %s:%d with applied Reality key: status=%d body=%q", host, port, status, body)
				}
			}
			assertNotServed := func() {
				t.Helper()
				status, body := fetch()
				if status == http.StatusOK || strings.Contains(body, "vless://") {
					t.Fatalf("unapplied account must not be served: status=%d body=%q", status, body)
				}
			}
			move := func(serverID string) {
				t.Helper()
				if _, err := accounts.UpdateAccount(ctx, accountID, vpnaccounts.UpdateAccountInput{ServerID: &serverID}); err != nil {
					t.Fatalf("assign account to %s: %v", serverID, err)
				}
			}
			assertSameToken := func() {
				t.Helper()
				token, err := accounts.GetActiveDeviceSubscriptionToken(ctx, device.ID)
				if err != nil || token.ID != originalToken.ID {
					t.Fatalf("assignment/deployment rotated or removed device token: got=%+v err=%v", token, err)
				}
			}

			assertServed("203.0.113.10", tc.sourcePort, sourceKey)

			move(targetID)
			assertSameToken()
			assertNotServed() // Existing reassignment is NOT zero-downtime.

			targetV1 := renderVersion(t, ctx, render, targetID)
			finishApply(t, ctx, pool, targetID, targetV1, "failed")
			assertNotServed()
			assertSameToken()

			finishApply(t, ctx, pool, targetID, targetV1, "succeeded")
			assertServed("203.0.113.20", tc.targetPort, targetKey)
			assertSameToken()

			// Old-node cleanup happens after target apply, independently of role.
			sourceV2 := renderVersion(t, ctx, render, sourceID)
			finishApply(t, ctx, pool, sourceID, sourceV2, "succeeded")
			assertServed("203.0.113.20", tc.targetPort, targetKey)

			move(sourceID)
			assertNotServed() // Old node no longer holds the account.
			sourceV3 := renderVersion(t, ctx, render, sourceID)
			finishApply(t, ctx, pool, sourceID, sourceV3, "succeeded")
			assertServed("203.0.113.10", tc.sourcePort, sourceKey)
			assertSameToken()
		})
	}
}

func createReassignmentTarget(t *testing.T, ctx context.Context, pool *pgxpool.Pool, key realityTestKey) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(ctx, `
		INSERT INTO servers (
			name, status, deployment_role, public_ip, vpn_protocol, vless_port,
			vless_flow, vless_network, reality_private_key, reality_public_key,
			reality_short_id, reality_server_name
		) VALUES (
			'rg-140-target', 'active', 'vpn', '203.0.113.20', 'vless', 8443,
			'xtls-rprx-vision', 'tcp', $1, $2,
			'fedcba9876543210', 'www.microsoft.com'
		) RETURNING id::text
	`, key.private, key.public).Scan(&id); err != nil {
		t.Fatalf("create reassignment target: %v", err)
	}
	return id
}

func setReassignmentNodeTopology(t *testing.T, ctx context.Context, pool *pgxpool.Pool, serverID, role string, port int) {
	t.Helper()
	if _, err := pool.Exec(ctx, `
		UPDATE servers SET deployment_role = $2, vless_port = $3
		WHERE id = $1::uuid
	`, serverID, role, port); err != nil {
		t.Fatalf("set %s node topology on port %d: %v", role, port, err)
	}
}
