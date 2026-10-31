package customercontact

import "testing"

func TestContactability(t *testing.T) {
	for _, tc := range []struct {
		name, channel, account, nickname string
		want                             bool
	}{
		{"nickname only", "小红书", "", "张小姐爱生活", true},
		{"account only", "WECHAT", "WX-123", "", true},
		{"moments nickname", "朋友圈", "", "张小姐", true},
		{"enterprise wechat", "企微", "", "张小姐", true},
		{"no contact clue", "douyin", " ", " ", false},
		{"unknown platform", "unknown", "", "张小姐", false},
		{"generic social source", "social_media", "123", "张小姐", false},
		{"generic chat source", "聊天软件", "", "张小姐", false},
		{"internal patient number", "patient", "MRN-123", "张小姐", false},
		{"generated identifier", "__auto__", "123", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := Contact{ChannelType: tc.channel, ExternalID: tc.account, ExternalName: tc.nickname}
			if got := c.CanContact(); got != tc.want {
				t.Fatalf("CanContact() = %v, want %v", got, tc.want)
			}
		})
	}
}
