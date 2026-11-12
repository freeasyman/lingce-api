package billing_test

// 行业/科室字典与渠道/销售的增删改查集成测试。

import (
	"errors"
	"strings"
	"testing"

	"github.com/freeasyman/lingce-api/internal/billing"
)

func boolPtr(value bool) *bool { return &value }

func TestBillingDictionaryAndChannel(t *testing.T) {
	env := newBillingTestEnv(t)
	ctx := env.ctx

	// ---------- 行业 / 科室字典 ----------
	dictionaries, err := env.svc.GetDictionaries(ctx)
	if err != nil {
		t.Fatalf("读取字典失败: %v", err)
	}
	if len(dictionaries.Industries) < 17 || len(dictionaries.Departments) < 19 {
		t.Fatalf("字典种子不足：行业 %d、科室 %d", len(dictionaries.Industries), len(dictionaries.Departments))
	}
	if dictionaries.IsMock {
		t.Fatalf("真实接口不应返回 isMock=true")
	}

	industryName := "测试行业-" + env.suffix
	created, err := env.svc.SaveDictItem(ctx, billing.DictKindIndustry, billing.DictItemPayload{
		Name: industryName, Order: 900,
	}, env.adminID)
	if err != nil {
		t.Fatalf("新增行业失败: %v", err)
	}
	t.Cleanup(func() {
		// 改名后的条目也以「测试行业/测试科室」开头，用宽前缀保证清理干净。
		_, _ = env.pool.Exec(ctx, `DELETE FROM billing_industry_dict WHERE name LIKE $1`, "测试行业%")
		_, _ = env.pool.Exec(ctx, `DELETE FROM billing_department_dict WHERE name LIKE $1`, "测试科室%")
	})
	if !created.Active || created.Order != 900 {
		t.Fatalf("新增行业字段不对: %+v", created)
	}

	// 重名 -> conflict
	if _, err := env.svc.SaveDictItem(ctx, billing.DictKindIndustry, billing.DictItemPayload{
		Name: industryName, Order: 910,
	}, env.adminID); err == nil {
		t.Fatalf("重名字典项应返回冲突")
	} else {
		var conflict *billing.ConflictError
		if !errors.As(err, &conflict) {
			t.Fatalf("重名字典项应返回 ConflictError，实际 %T: %v", err, err)
		}
	}

	// 改名 + 排序
	renamedName := "测试行业改-" + env.suffix
	updated, err := env.svc.SaveDictItem(ctx, billing.DictKindIndustry, billing.DictItemPayload{
		ID: created.ID, Name: renamedName, Order: 20,
	}, env.adminID)
	if err != nil {
		t.Fatalf("修改行业失败: %v", err)
	}
	if updated.Name != renamedName || updated.Order != 20 {
		t.Fatalf("修改行业字段不对: %+v", updated)
	}

	// 启停（两次切换回原状态）
	toggled, err := env.svc.ToggleDictItem(ctx, billing.DictKindIndustry, created.ID, env.adminID)
	if err != nil {
		t.Fatalf("停用行业失败: %v", err)
	}
	if toggled.Active {
		t.Fatalf("停用后 active 应为 false: %+v", toggled)
	}
	toggled, err = env.svc.ToggleDictItem(ctx, billing.DictKindIndustry, created.ID, env.adminID)
	if err != nil {
		t.Fatalf("启用行业失败: %v", err)
	}
	if !toggled.Active {
		t.Fatalf("启用后 active 应为 true: %+v", toggled)
	}

	// 科室字典新增
	departmentName := "测试科室-" + env.suffix
	department, err := env.svc.SaveDictItem(ctx, billing.DictKindDepartment, billing.DictItemPayload{
		Name: departmentName, Order: 900,
	}, env.adminID)
	if err != nil {
		t.Fatalf("新增科室失败: %v", err)
	}
	if department.Name != departmentName {
		t.Fatalf("新增科室字段不对: %+v", department)
	}

	// 字典类型非法 -> 400
	if _, err := env.svc.SaveDictItem(ctx, "other", billing.DictItemPayload{Name: "x"}, env.adminID); err == nil {
		t.Fatalf("非法字典类型应被拒绝")
	}
	// 不存在的字典项 -> not found
	if _, err := env.svc.ToggleDictItem(ctx, billing.DictKindIndustry, 999999999, env.adminID); err == nil {
		t.Fatalf("不存在的字典项应返回 not found")
	}

	// ---------- 渠道 / 销售 ----------
	directory, err := env.svc.GetChannelDirectory(ctx)
	if err != nil {
		t.Fatalf("读取渠道目录失败: %v", err)
	}
	if directory.IsMock {
		t.Fatalf("真实接口不应返回 isMock=true")
	}

	channelName := "接口渠道-" + env.suffix
	channel, err := env.svc.SaveChannel(ctx, billing.ChannelPayload{
		Name: channelName, Type: "个人代理", Active: boolPtr(true),
	}, env.adminID)
	if err != nil {
		t.Fatalf("新增渠道失败: %v", err)
	}
	env.channelIDs = append(env.channelIDs, channel.ID)
	if channel.Type != "个人代理" || !channel.Active || channel.Name != channelName {
		t.Fatalf("新增渠道字段不对: %+v", channel)
	}

	// 渠道重名 -> conflict
	if _, err := env.svc.SaveChannel(ctx, billing.ChannelPayload{
		Name: channelName, Type: "渠道商", Active: boolPtr(true),
	}, env.adminID); err == nil {
		t.Fatalf("渠道重名应返回冲突")
	}

	// 新增销售
	salesName := "接口销售-" + env.suffix
	salesPhone := env.nextPhone("16")
	sales, err := env.svc.SaveSales(ctx, billing.SalesPayload{
		ChannelID: channel.ID, Name: salesName, Phone: salesPhone, Active: boolPtr(true),
	}, env.adminID)
	if err != nil {
		t.Fatalf("新增销售失败: %v", err)
	}
	env.salesIDs = append(env.salesIDs, sales.ID)
	if sales.ChannelID != channel.ID || sales.ChannelName != channelName || sales.Name != salesName {
		t.Fatalf("新增销售字段不对: %+v", sales)
	}

	// 销售手机号重复 -> conflict
	if _, err := env.svc.SaveSales(ctx, billing.SalesPayload{
		ChannelID: channel.ID, Name: salesName + "2", Phone: salesPhone, Active: boolPtr(true),
	}, env.adminID); err == nil {
		t.Fatalf("销售手机号重复应返回冲突")
	} else if !strings.Contains(err.Error(), "手机号") {
		t.Fatalf("销售手机号冲突提示不对: %v", err)
	}

	// 销售换渠道（更新）
	updatedSales, err := env.svc.SaveSales(ctx, billing.SalesPayload{
		ID: sales.ID, ChannelID: env.orgID, Name: salesName, Phone: salesPhone, Active: boolPtr(false),
	}, env.adminID)
	if err != nil {
		t.Fatalf("修改销售失败: %v", err)
	}
	if updatedSales.ChannelID != env.orgID || updatedSales.Active {
		t.Fatalf("修改销售字段不对: %+v", updatedSales)
	}

	// 停用渠道
	disabled, err := env.svc.SaveChannel(ctx, billing.ChannelPayload{
		ID: channel.ID, Name: channelName + "改", Type: "渠道商", Active: boolPtr(false),
	}, env.adminID)
	if err != nil {
		t.Fatalf("停用渠道失败: %v", err)
	}
	if disabled.Active || disabled.Type != "渠道商" {
		t.Fatalf("停用渠道字段不对: %+v", disabled)
	}

	// 目录里能看到刚改过的渠道
	directory, err = env.svc.GetChannelDirectory(ctx)
	if err != nil {
		t.Fatalf("再次读取渠道目录失败: %v", err)
	}
	found := false
	for _, item := range directory.Channels {
		if item.ID == channel.ID {
			found = true
			if item.Active {
				t.Fatalf("目录里该渠道应为停用: %+v", item)
			}
		}
	}
	if !found {
		t.Fatalf("渠道目录没有找到新建渠道")
	}
}
