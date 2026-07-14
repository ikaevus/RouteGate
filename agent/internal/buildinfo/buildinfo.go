package buildinfo

const AgentProtocolVersion = 1

var (
	Version   = "dev"
	GitCommit = "unknown"
	BuildDate = "unknown"
)

type Info struct {
	Version         string
	GitCommit       string
	BuildDate       string
	ProtocolVersion int
}

func Current() Info {
	return Info{
		Version:         valueOrDefault(Version, "dev"),
		GitCommit:       valueOrDefault(GitCommit, "unknown"),
		BuildDate:       valueOrDefault(BuildDate, "unknown"),
		ProtocolVersion: AgentProtocolVersion,
	}
}

func valueOrDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
