package vpnaccounts

func init() {
	// HAPP is an explicitly selectable RG-116 access client. Keeping this
	// registration beside the HAPP capability model avoids widening the device
	// allow-list to arbitrary Generic clients while preserving existing device
	// validation semantics.
	allowedDeviceClientTypes[ClientTypeHAPP] = struct{}{}
}
