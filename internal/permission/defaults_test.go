package permission_test

import (
	"slices"
	"testing"

	"github.com/freeasyman/lingce-api/internal/compliancepermission"
	"github.com/freeasyman/lingce-api/internal/dealexpertpermission"
	"github.com/freeasyman/lingce-api/internal/emrpermission"
	"github.com/freeasyman/lingce-api/internal/followuppermission"
	"github.com/freeasyman/lingce-api/internal/patientops"
	"github.com/freeasyman/lingce-api/internal/permission"
)

// TestAdminAbilityDefaultsMatchProductPackages 防止默认发放的能力码与各产品
// 权限页/校验逻辑漂移：默认发放写进库里的能力必须能被产品完整识别。
func TestAdminAbilityDefaultsMatchProductPackages(t *testing.T) {
	cases := []struct {
		product string
		want    []string
	}{
		{product: permission.ProductEMR, want: emrpermission.AllAbilities},
		{product: permission.ProductCompliance, want: compliancepermission.AllAbilities},
		{product: permission.ProductFollowup, want: followuppermission.AllAbilities},
		{product: permission.ProductPatientOps, want: patientops.AllPatientOpsAbilities},
		{product: permission.ProductDealExpert, want: dealexpertpermission.AllAbilities},
	}
	for _, tc := range cases {
		got := permission.AdminAbilities(tc.product)
		if !slices.Equal(got, tc.want) {
			t.Fatalf("%s admin abilities mismatch:\n got: %v\nwant: %v", tc.product, got, tc.want)
		}
	}
	if got := permission.AdminAbilities(permission.ProductContent); got != nil {
		t.Fatalf("content admin abilities = %v, want nil (no assignment table)", got)
	}
}
