// Package pdf 渲染运营端计费业务的 A4 单页通知函（试用开通通知函 / 服务开通确认函）。
//
// 设计来源：06-原型/运营端计费Demo.html 的 .letter-page 版式，成品参考同目录下的
// 样例-试用开通通知函.pdf / 样例-服务开通确认函.pdf。
//
// 约定：
//   - 输入只有 Letter 这一个纯数据 DTO，渲染过程不查库、不读配置文件、不依赖外部进程；
//   - 中文用内嵌的 Noto Sans SC（OFL 1.1）做子集嵌入，任何环境都不会乱码；
//   - 输出恒为 A4 单页：内容超高时自动按比例缩小字号与间距（见 render.go 的 scale 循环），
//     不会溢出到第二页；
//   - 中英文混排的换行由本包按字符宽度自行断行（见 wrap.go），不依赖 gofpdf 的空格断行。
package pdf

import (
	"errors"
	"strings"
)

var (
	// ErrFontUnavailable 表示内嵌中文字体注册失败（生成出来必然乱码，直接失败）。
	ErrFontUnavailable = errors.New("billing pdf: 内嵌中文字体加载失败")
	// ErrEmptyLetter 表示 DTO 缺少必要内容。
	ErrEmptyLetter = errors.New("billing pdf: 通知函内容为空")
	// ErrContentOverflow 表示按最小缩放仍放不进 A4 单页。
	ErrContentOverflow = errors.New("billing pdf: 内容无法排进 A4 单页")
)

// Letter 是一份通知函的全部展示内容（纯数据，不含任何业务判断）。
//
// 文案里可以用 **文字** 标记加粗片段（例如网址、手机号），渲染时解析成富文本。
type Letter struct {
	// DocNo / DocDate 显示在页眉右上角：编号 LC-TR-2026-0005、日期 2026-11-12。
	DocNo   string `json:"docNo"`
	DocDate string `json:"docDate"`

	// Title 是居中大标题：试用开通通知函 / 服务开通确认函。
	Title string `json:"title"`
	// Salutation 是抬头整行，例如「尊敬的　天津灵策眼科　李强（院长）：」。
	Salutation string `json:"salutation"`
	// Intro 是引言段落。
	Intro string `json:"intro"`

	// InfoSection 是「试用信息 / 服务信息」小标题与其下的两行信息盒。
	InfoSection string    `json:"infoSection"`
	InfoRows    []InfoRow `json:"infoRows"`

	// ProductSection 是「试用内容 / 服务内容」小标题与产品额度表。
	ProductSection string `json:"productSection"`
	QuotaHeader    string `json:"quotaHeader"`
	// ProductColumnLabel 是内容表第一列表头，默认「产品」（换新通知单用「项目」）。
	ProductColumnLabel string       `json:"productColumnLabel,omitempty"`
	Products           []ProductRow `json:"products"`

	// 开始使用与温馨提示：items 按顺序编号（圆形序号），支持 **加粗** 片段。
	StartSection string   `json:"startSection"`
	StartItems   []string `json:"startItems"`
	TipSection   string   `json:"tipSection"`
	TipItems     []string `json:"tipItems"`

	// Advisor 是页脚左侧的顾问卡，只显示姓名与电话。
	Advisor Advisor `json:"advisor"`
	// AdvisorSection 是顾问卡上方的小标题（默认「您的专属顾问」），样式与其它小标题一致。
	AdvisorSection string `json:"advisorSection"`

	// Company 是落款公司全称；SignDate 是落款日期（如 2026 年 11 月 12 日）。
	Company  string `json:"company"`
	SignDate string `json:"signDate"`

	// FooterLeft / FooterRight 是页脚两端的文字。
	FooterLeft  string `json:"footerLeft"`
	FooterRight string `json:"footerRight"`

	// SealLines 是电子章内的文字行，默认 ["灵策", "电子章"]。
	SealLines []string `json:"sealLines"`
	// SealOverCompany 为 true 时，电子章盖在公司名中间偏左，而不是名字右侧。
	SealOverCompany bool `json:"sealOverCompany,omitempty"`
	// RealSeal 为 true 时使用实拍公章图片，而不是矢量绘制的电子章。
	RealSeal bool `json:"realSeal,omitempty"`
}

// InfoRow 是信息盒里的一行「标签 + 值」。
type InfoRow struct {
	Label     string `json:"label"`
	Value     string `json:"value"`
	ValueBold bool   `json:"valueBold"`
}

// ProductRow 是内容表里的一行「产品 + 额度」。
type ProductRow struct {
	Name  string `json:"name"`
	Quota string `json:"quota"`
}

// Advisor 是顾问卡内容（不写渠道/组织）。
type Advisor struct {
	Name  string `json:"name"`
	Phone string `json:"phone"`
	// IsBrand 表示没有具体员工顾问，头像位置画灵策品牌标记而不是取名字首字。
	IsBrand bool `json:"isBrand"`
}

// validate 校验渲染所需的最小内容集合，缺内容时直接报错，避免生成空白函。
func (l Letter) validate() error {
	switch {
	case strings.TrimSpace(l.Title) == "":
		return ErrEmptyLetter
	case strings.TrimSpace(l.Salutation) == "":
		return ErrEmptyLetter
	case strings.TrimSpace(l.Intro) == "":
		return ErrEmptyLetter
	case strings.TrimSpace(l.Company) == "":
		return ErrEmptyLetter
	case len(l.InfoRows) == 0:
		return ErrEmptyLetter
	case len(l.Products) == 0:
		return ErrEmptyLetter
	}
	return nil
}

// advisorSectionTitle 返回顾问卡小标题（未配置时用「您的专属顾问」）。
func (l Letter) advisorSectionTitle() string {
	if title := strings.TrimSpace(l.AdvisorSection); title != "" {
		return title
	}
	return "您的专属顾问"
}

// sealLines 返回电子章文字行（未配置时用默认值）。
func (l Letter) sealLines() []string {
	if len(l.SealLines) > 0 {
		return l.SealLines
	}
	return []string{"灵策", "电子章"}
}
