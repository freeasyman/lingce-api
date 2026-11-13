package his

import (
	"strconv"
	"strings"
)

// icd10Category 根据 ICD-10 编码推导章节分类。
// emr_icd10_diagnosis_catalog 当前只导入了编码/名称，category_name 等列为空，
// 字典中心仍需要展示“分类”，因此按编码首字母 + 数字区间做本地章节映射（不写库）。
func icd10Category(code string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	if len(code) < 2 {
		return ""
	}
	letter := code[0]
	digits := 0
	for digits < len(code)-1 && code[1+digits] >= '0' && code[1+digits] <= '9' {
		digits++
	}
	number := -1
	if digits > 0 {
		if value, err := strconv.Atoi(code[1 : 1+digits]); err == nil {
			number = value
		}
	}
	switch letter {
	case 'A', 'B':
		return "某些传染病和寄生虫病"
	case 'C':
		return "肿瘤"
	case 'D':
		if number >= 50 {
			return "血液及造血器官疾病和某些涉及免疫机制的疾患"
		}
		return "肿瘤"
	case 'E':
		return "内分泌、营养和代谢疾病"
	case 'F':
		return "精神和行为障碍"
	case 'G':
		return "神经系统疾病"
	case 'H':
		if number >= 60 {
			return "耳和乳突疾病"
		}
		return "眼和附器疾病"
	case 'I':
		return "循环系统疾病"
	case 'J':
		return "呼吸系统疾病"
	case 'K':
		return "消化系统疾病"
	case 'L':
		return "皮肤和皮下组织疾病"
	case 'M':
		return "肌肉骨骼系统和结缔组织疾病"
	case 'N':
		return "泌尿生殖系统疾病"
	case 'O':
		return "妊娠、分娩和产褥期"
	case 'P':
		return "起源于围生期的某些情况"
	case 'Q':
		return "先天性畸形、变形和染色体异常"
	case 'R':
		return "症状、体征和临床与实验室异常所见"
	case 'S', 'T':
		return "损伤、中毒和外因的某些其他后果"
	case 'V', 'W', 'X', 'Y':
		return "疾病和死亡的外因"
	case 'Z':
		return "影响健康状态和与保健机构接触的因素"
	case 'U':
		return "特殊目的编码"
	default:
		return ""
	}
}
