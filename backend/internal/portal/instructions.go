package portal

var instructionPlatforms = []InstructionPlatform{
	{Platform: "ios", DisplayName: "iOS", Description: "Use the direct QR profile with an app compatible with the protocol shown in RouteGate."},
	{Platform: "android", DisplayName: "Android", Description: "Use the direct QR profile with an app compatible with the protocol shown in RouteGate."},
	{Platform: "windows", DisplayName: "Windows", Description: "Import the direct connection material into a compatible desktop client."},
	{Platform: "macos", DisplayName: "macOS", Description: "Import the direct connection material into a compatible macOS client."},
	{Platform: "linux", DisplayName: "Linux", Description: "Import the direct connection material into a compatible Linux client."},
}

var instructionsByPlatform = map[string]DeviceInstruction{
	"ios": {
		Platform:    "ios",
		DisplayName: "iOS",
		Steps: []string{
			"Open your RouteGate VPN profile and check the protocol shown for it.",
			"Install an app compatible with that protocol. For MTProto, use Telegram.",
			"Open the direct connection QR code in RouteGate and scan it with the compatible app, or copy the QR payload for manual import.",
			"Confirm the imported profile or proxy settings in the app.",
			"Enable the connection and test it.",
		},
		Notes: []string{
			"RouteGate supports multiple connection protocols; the protocol shown in the profile determines client compatibility.",
			"The RouteGate subscription URL is an advanced RouteGate format and is separate from the direct connection QR code.",
		},
	},
	"android": {
		Platform:    "android",
		DisplayName: "Android",
		Steps: []string{
			"Open your RouteGate VPN profile and check the protocol shown for it.",
			"Install an app compatible with that protocol. For MTProto, use Telegram.",
			"Open the direct connection QR code in RouteGate and scan it with the compatible app, or copy the QR payload for manual import.",
			"Confirm the imported profile or proxy settings in the app.",
			"Enable the connection and test it.",
		},
		Notes: []string{
			"Some Android clients may require VPN permission confirmation from the system.",
			"The RouteGate subscription URL is an advanced RouteGate format and is separate from the direct connection QR code.",
		},
	},
	"windows": {
		Platform:    "windows",
		DisplayName: "Windows",
		Steps: []string{
			"Open your RouteGate VPN profile and check the protocol shown for it.",
			"Install a desktop client compatible with that protocol. For MTProto, use Telegram Desktop.",
			"Open the direct connection QR code and scan it when the client supports QR import, or copy the QR payload and import it manually.",
			"Confirm the imported profile or proxy settings.",
			"Enable the connection and test it.",
		},
		Notes: []string{
			"Client recommendations are intentionally not tied to one commercial application.",
			"The RouteGate subscription URL is an advanced RouteGate format and is separate from direct client connection material.",
		},
	},
	"macos": {
		Platform:    "macos",
		DisplayName: "macOS",
		Steps: []string{
			"Open your RouteGate VPN profile and check the protocol shown for it.",
			"Install a macOS client compatible with that protocol. For MTProto, use Telegram.",
			"Open the direct connection QR code and scan it when supported, or copy its payload for manual import.",
			"Confirm the imported profile or proxy settings.",
			"Enable the connection and test it.",
		},
		Notes: []string{
			"If the client asks for network extension permission, approve it in macOS settings.",
			"The RouteGate subscription URL is an advanced RouteGate format and is separate from direct client connection material.",
		},
	},
	"linux": {
		Platform:    "linux",
		DisplayName: "Linux",
		Steps: []string{
			"Open your RouteGate VPN profile and check the protocol shown for it.",
			"Install a Linux client compatible with that protocol. For MTProto, use a Telegram client.",
			"Open the direct connection QR code and copy its payload or configuration into the client.",
			"Import or save the profile according to the client documentation.",
			"Start the connection and test it.",
		},
		Notes: []string{
			"Linux setup may require elevated permissions depending on the client and network mode.",
			"The RouteGate subscription URL is an advanced RouteGate format and is separate from direct client connection material.",
		},
	},
}
