package his

import "errors"

// 未付单结账按当前身份重算：收费/合并下单时读取患者当前 price_identity，
// 用当前折率矩阵重算所有行，并保留议价 mode/value 按新的可折金额重新分摊。
//
// 规则：
//   - 仅 pending 单重算；paid/refunded/void 不重算；
//   - partial 单不重算；若当前身份与下单快照不同，返回 ErrOrderPartiallyPaid；
//   - 药品恒锁 1.0，材料/其他按当前矩阵；
//   - 金额议价超过新的可折金额时收口到可折金额，百分比议价按新基数重算。

// ErrOrderPartiallyPaid 表示部分收款后身份已变更，不能直接重算/继续收款。
// 需先结清或退费后再调整身份。
var ErrOrderPartiallyPaid = errors.New("order partially paid, settle or refund before identity change")

// shouldRepriceOrder 是否需要按当前身份重算：仅未收费（pending）订单。
func shouldRepriceOrder(status string) bool {
	return status == "pending"
}

// checkOrderIdentityChangeAllowed 校验状态与身份变更组合。
// partial 单若身份未变，仍允许正常补收；身份变了则必须先结清/退费。
func checkOrderIdentityChangeAllowed(status, orderIdentity, currentIdentity string) error {
	if status == "partial" && orderIdentity != currentIdentity {
		return ErrOrderPartiallyPaid
	}
	return nil
}

// orderRepricePlan 一次按当前身份重算的结果。
type orderRepricePlan struct {
	Rows             []orderItemRow
	Negotiation      orderNegotiation
	ItemsTotal       float64
	IdentityDiscount float64
	NegotiationTotal float64
	DiscountTotal    float64
	PayableTotal     float64
}

// negotiableBase 可议价行（身份折后）合计：药品/材料不参与。
func negotiableBase(rows []orderItemRow) float64 {
	total := 0.0
	for index := range rows {
		row := &rows[index]
		if !isNegotiableCategory(row.Category) {
			continue
		}
		base := discountMoney(row.OriginalTotal - row.DiscountAmount)
		if base > 0 {
			total += base
		}
	}
	return discountMoney(total)
}

// planOrderReprice 按给定折率矩阵重算所有行，并重摊议价。不读写数据库，便于单测。
func planOrderReprice(rows []orderItemRow, negotiation orderNegotiation, rates map[string]float64) (orderRepricePlan, error) {
	plan := orderRepricePlan{
		Rows:        make([]orderItemRow, len(rows)),
		Negotiation: negotiation,
	}
	copy(plan.Rows, rows)
	for index := range plan.Rows {
		row := &plan.Rows[index]
		row.OriginalTotal = orderItemRowOriginal(*row)
		row.Category = discountCategoryForLine(row.ItemType, row.Category)
		rate := rateFor(rates, row.Category)
		if row.Category == discountCategoryDrug {
			rate = 1
		}
		row.IdentityRate = rate
		row.DiscountAmount = discountMoney(row.OriginalTotal * (1 - rate))
		row.NegotiationAmount = 0
		row.LineTotal = discountMoney(row.OriginalTotal - row.DiscountAmount)
	}
	if plan.Negotiation.Mode == negotiationModeAmount && plan.Negotiation.Value > 0 {
		base := negotiableBase(plan.Rows)
		if plan.Negotiation.Value > base {
			plan.Negotiation.Value = base
		}
	}
	negotiationTotal, err := applyNegotiation(plan.Rows, plan.Negotiation.Mode, plan.Negotiation.Value)
	if err != nil {
		return orderRepricePlan{}, err
	}
	plan.ItemsTotal = orderRowsTotal(plan.Rows)
	plan.IdentityDiscount = orderRowsIdentityDiscount(plan.Rows)
	plan.NegotiationTotal = negotiationTotal
	plan.DiscountTotal = discountMoney(plan.IdentityDiscount + negotiationTotal)
	plan.PayableTotal = orderRowsPayable(plan.Rows)
	return plan, nil
}
