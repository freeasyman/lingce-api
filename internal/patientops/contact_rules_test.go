package patientops

import (
	"testing"

	"github.com/freeasyman/lingce-api/internal/customercontact"
)

func TestPlanContactExclusion(t *testing.T) {
	for _, tc := range []struct {
		name, phone, channel, nickname string
		excluded                       bool
	}{
		{"phone", "13800138000", "", "", false},
		{"xiaohongshu nickname", "", "xiaohongshu", "张小姐爱生活", false},
		{"wechat nickname", "", "微信", "张小姐", false},
		{"internal id", "", "patient", "病案号", true},
		{"unknown platform", "", "unknown", "张小姐", true},
		{"no clue", "", "wechat", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fact := planCustomerFact{Phone: tc.phone, Contacts: []customercontact.Contact{{ChannelType: tc.channel, ExternalName: tc.nickname}}}
			note := planSoftNote(fact)
			if (note == "缺少有效联系方式") != tc.excluded {
				t.Fatalf("soft note = %q, want excluded %v", note, tc.excluded)
			}
		})
	}
}
