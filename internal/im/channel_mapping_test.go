package im

import (
	"testing"

	"reliquary/internal/types"
)

// Files uploaded through an IM channel are tagged with a knowledge Channel.
// Lark deliberately shares Feishu's channel (types.ChannelFeishu documents
// itself as "Feishu / Lark"), so Lark uploads group with Feishu ones in the KB
// UI rather than falling through to the generic "im" channel.
func TestIMPlatformToChannel(t *testing.T) {
	cases := map[string]string{
		"feishu": types.ChannelFeishu,
		"lark":   types.ChannelFeishu,
		"Lark":   types.ChannelFeishu,
		"":       types.ChannelIM,
	}
	for platform, want := range cases {
		if got := imPlatformToChannel(platform); got != want {
			t.Errorf("imPlatformToChannel(%q) = %q, want %q", platform, got, want)
		}
	}
}
