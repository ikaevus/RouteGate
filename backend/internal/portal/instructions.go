package portal

var instructionPlatforms = []InstructionPlatform{
	{Platform: "ios", DisplayName: "iOS", Description: "Import a subscription link or QR code on iPhone and iPad."},
	{Platform: "android", DisplayName: "Android", Description: "Import a subscription link or QR code on Android devices."},
	{Platform: "windows", DisplayName: "Windows", Description: "Import a subscription link in a compatible desktop client."},
	{Platform: "macos", DisplayName: "macOS", Description: "Import a subscription link in a compatible macOS client."},
	{Platform: "linux", DisplayName: "Linux", Description: "Import a subscription link in a compatible Linux client."},
}

var instructionsByPlatform = map[string]DeviceInstruction{
	"ios": {
		Platform:    "ios",
		DisplayName: "iOS",
		Steps: []string{
			"Install a compatible VLESS or sing-box client.",
			"Open your RouteGate VPN profile in the User Portal.",
			"Scan the QR code or copy the subscription link.",
			"Import the profile in the client application.",
			"Enable the imported profile and test the connection.",
		},
		Notes: []string{
			"RouteGate does not require one specific commercial client.",
			"Client recommendations should remain configurable by the administrator.",
		},
	},
	"android": {
		Platform:    "android",
		DisplayName: "Android",
		Steps: []string{
			"Install a compatible VLESS or sing-box client.",
			"Open your RouteGate VPN profile in the User Portal.",
			"Scan the QR code or copy the subscription link.",
			"Import the subscription in the client application.",
			"Enable the imported profile and test the connection.",
		},
		Notes: []string{
			"Some Android clients may require VPN permission confirmation from the system.",
		},
	},
	"windows": {
		Platform:    "windows",
		DisplayName: "Windows",
		Steps: []string{
			"Install a compatible desktop VPN client.",
			"Copy the subscription link from your RouteGate profile.",
			"Import the subscription link in the client application.",
			"Refresh the subscription if the client supports it.",
			"Enable the imported profile and test the connection.",
		},
		Notes: []string{
			"Desktop client recommendations are intentionally kept configurable.",
		},
	},
	"macos": {
		Platform:    "macos",
		DisplayName: "macOS",
		Steps: []string{
			"Install a compatible macOS VPN client.",
			"Copy the subscription link from your RouteGate profile.",
			"Import the subscription link in the client application.",
			"Enable the imported profile and test the connection.",
		},
		Notes: []string{
			"If the client asks for network extension permission, approve it in macOS settings.",
		},
	},
	"linux": {
		Platform:    "linux",
		DisplayName: "Linux",
		Steps: []string{
			"Install a compatible Linux client or sing-box based client workflow.",
			"Copy the subscription link from your RouteGate profile.",
			"Import or render the subscription according to the client documentation.",
			"Start the client and test the connection.",
		},
		Notes: []string{
			"Linux setup may require elevated permissions depending on the client and network mode.",
		},
	},
}
