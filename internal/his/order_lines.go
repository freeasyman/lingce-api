package his

// 收费行类型：前端表单是唯一真相，逐行显式提交；服务端只做校验与定价（拒绝前端传价）。
const (
	orderItemChargeItem      = "charge_item"      // 字典收费项目：按 ref_id 取字典价
	orderItemBundle          = "bundle"           // 打包项目：按 ref_id 取打包价
	orderItemDrug            = "drug"             // 处方药品：按 ref_id 取药品价，可带 prescription_id
	orderItemRegistrationFee = "registration_fee" // 挂号费：按就诊对应挂号记录的 fee 定价，忽略 ref_id
)

// registrationFeeLineName 挂号费行名称，不依赖收费项目字典里是否存在同名项目。
const registrationFeeLineName = "挂号费"

// validateOrderLineInputs 校验收费行输入，不查库（纯逻辑，便于单测）。
func validateOrderLineInputs(inputs []OrderLineInput) error {
	if len(inputs) == 0 {
		return ErrInvalidRequest
	}
	for _, input := range inputs {
		// 数量必须为正；行优惠不能为负；行优惠超过行金额由定价阶段兜底。
		if input.Quantity <= 0 || input.DiscountAmount < 0 {
			return ErrInvalidRequest
		}
		switch input.ItemType {
		case orderItemChargeItem, orderItemBundle, orderItemDrug:
			if input.RefID <= 0 {
				return ErrInvalidRequest
			}
		case orderItemRegistrationFee:
			// 挂号费只由就诊的挂号记录定价，ref_id/prescription_id 均无意义。
		default:
			return ErrInvalidRequest
		}
	}
	return nil
}

// registrationFeeLine 由挂号记录的 fee 生成挂号费行：行名固定「挂号费」，金额不来自收费项目字典。
func registrationFeeLine(fee float64) (resolvedLine, error) {
	if fee < 0 {
		return resolvedLine{}, ErrInvalidRequest
	}
	return resolvedLine{Name: registrationFeeLineName, UnitPrice: fee}, nil
}

// drugLinePrice 药品行定价：处方明细快照价优先（开方时的价格），处方未含该药时回退药品字典价。
// 这样弹窗预览（处方明细价）与后端计费一致，开方后改药品字典价不影响已开处方。
func drugLinePrice(dict resolvedLine, snapshot *resolvedLine) (resolvedLine, error) {
	if dict.UnitPrice < 0 {
		return resolvedLine{}, ErrInvalidRequest
	}
	if snapshot == nil {
		return dict, nil
	}
	if snapshot.UnitPrice < 0 {
		return resolvedLine{}, ErrInvalidRequest
	}
	line := dict
	line.Name = snapshot.Name
	line.UnitPrice = snapshot.UnitPrice
	return line, nil
}

// orderItemRow 已定价的收费行（价格全部来自服务端解析结果）。
type orderItemRow struct {
	ItemType       string
	RefID          *int64
	PrescriptionID *int64
	Name           string
	UnitPrice      float64
	Quantity       float64
	DiscountAmount float64
	LineTotal      float64
}

// orderLineAmount 行小计 = 单价 × 数量 − 行优惠。
func orderLineAmount(unitPrice, quantity, discount float64) (float64, error) {
	if unitPrice < 0 || quantity <= 0 || discount < 0 {
		return 0, ErrInvalidRequest
	}
	total := unitPrice*quantity - discount
	if total < 0 {
		return 0, ErrInvalidRequest
	}
	return total, nil
}

// orderPayable 应收 = 行合计 − 整单优惠。
func orderPayable(itemsTotal, discountTotal float64) (float64, error) {
	if itemsTotal < 0 || discountTotal < 0 {
		return 0, ErrInvalidRequest
	}
	payable := itemsTotal - discountTotal
	if payable < 0 {
		return 0, ErrInvalidRequest
	}
	return payable, nil
}

// buildOrderItemRows 逐行定价并生成待落库的收费行。
// resolve 由 Store 提供（字典价 / 打包价 / 药品价 / 挂号记录挂号费），本条链路不接受前端传价。
func buildOrderItemRows(inputs []OrderLineInput, resolve func(OrderLineInput) (resolvedLine, error)) ([]orderItemRow, error) {
	if err := validateOrderLineInputs(inputs); err != nil {
		return nil, err
	}
	rows := make([]orderItemRow, 0, len(inputs))
	for _, input := range inputs {
		line, err := resolve(input)
		if err != nil {
			return nil, err
		}
		if line.UnitPrice < 0 {
			return nil, ErrInvalidRequest
		}
		lineTotal, err := orderLineAmount(line.UnitPrice, input.Quantity, input.DiscountAmount)
		if err != nil {
			return nil, err
		}
		row := orderItemRow{
			ItemType:       input.ItemType,
			Name:           line.Name,
			UnitPrice:      line.UnitPrice,
			Quantity:       input.Quantity,
			DiscountAmount: input.DiscountAmount,
			LineTotal:      lineTotal,
		}
		if input.ItemType == orderItemRegistrationFee {
			// 挂号费不挂字典项目/处方，ref_id 与 prescription_id 一律丢弃。
			rows = append(rows, row)
			continue
		}
		if input.RefID > 0 {
			refID := input.RefID
			row.RefID = &refID
		}
		row.PrescriptionID = line.PrescriptionID
		rows = append(rows, row)
	}
	return rows, nil
}
