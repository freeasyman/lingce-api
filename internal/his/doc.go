// Package his 是灵策门诊 HIS 的后端域（P1 标准诊疗闭环）。
// 设计基线：lingce-web/docs/his/02-域模型-v0.1。
// 原则：复用 customers/encounters/emr_records；HIS 是业务发生系统，
// 病历域生产者，听写病历产品是工具 + 质控器。
package his
