package agents

import "testing"

func TestHashToken(t *testing.T) {
	t.Parallel()

	const want = "4d639a937318e7110946aa9f9afbd5d0ace3e03bbd80a580774abaa3f2398cd9"
	if got := HashToken("routegate-agent-token"); got != want {
		t.Fatalf("HashToken() = %q, want %q", got, want)
	}
}
