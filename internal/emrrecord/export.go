package emrrecord

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/freeasyman/lingce-api/internal/emrprocess"
	"github.com/xuri/excelize/v2"
)

// ProcessRecords 供导出等只读场景使用。
func (s *Service) ProcessRecords(ctx context.Context, tenantID int64, id string) ([]*emrprocess.ProcessRecord, error) {
	return s.process.List(ctx, tenantID, id)
}

var auditFieldLabels = map[string]string{
	"chief_complaint": "主诉", "present_illness": "现病史", "past_history": "既往史",
	"personal_history": "个人史", "family_history": "家族史", "allergy_history": "过敏史",
	"physical_exam": "体格检查", "auxiliary_exam": "辅助检查", "diagnosis": "诊断",
	"prescription": "处方", "disposition": "处置", "medical_advice": "医嘱",
	"followup": "复诊安排", "tcm_diagnosis": "中医诊断", "tcm_four_examinations": "中医四诊",
	"symptom": "症状", "sign": "体征", "remark": "备注",
}

func auditFieldLabel(code string) string {
	if label, ok := auditFieldLabels[code]; ok {
		return label
	}
	return code
}

func auditJSON(value any) string {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return ""
	}
	text := strings.TrimSpace(buffer.String())
	if len(text) > 32000 {
		text = text[:32000] + "…(内容过长已截断)"
	}
	return text
}

func auditString(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

func auditStatusPair(before, after *string) string {
	left, right := "-", "-"
	if before != nil && strings.TrimSpace(*before) != "" {
		left = *before
	}
	if after != nil && strings.TrimSpace(*after) != "" {
		right = *after
	}
	return left + " → " + right
}

// buildAuditWorkbook 生成三张表：操作日志、版本正文、字段变更明细。
func buildAuditWorkbook(record *Record, snapshots []*Snapshot, processes []*emrprocess.ProcessRecord) (*excelize.File, error) {
	file := excelize.NewFile()
	const logsSheet, versionsSheet, changesSheet = "操作日志", "版本正文", "字段变更明细"
	if err := file.SetSheetName("Sheet1", logsSheet); err != nil {
		return nil, err
	}
	if _, err := file.NewSheet(versionsSheet); err != nil {
		return nil, err
	}
	if _, err := file.NewSheet(changesSheet); err != nil {
		return nil, err
	}

	snapshotByID := make(map[string]*Snapshot, len(snapshots))
	for _, snapshot := range snapshots {
		snapshotByID[snapshot.ID] = snapshot
	}
	actorNames := make(map[int64]string)
	for _, process := range processes {
		if process.ActorID != nil && strings.TrimSpace(process.ActorName) != "" {
			actorNames[*process.ActorID] = process.ActorName
		}
	}
	patientName := auditString(record.PatientSnapshot["name"])
	doctorName := auditString(record.EncounterContext["doctor_name"])
	if doctorName == "" {
		doctorName = fmt.Sprintf("医生 %d", record.DoctorID)
	}
	department := auditString(record.EncounterContext["medical_department_code"])
	if department == "" && record.DepartmentID != nil {
		department = fmt.Sprintf("科室 %d", *record.DepartmentID)
	}

	sortedProcesses := append([]*emrprocess.ProcessRecord(nil), processes...)
	sort.SliceStable(sortedProcesses, func(i, j int) bool {
		return sortedProcesses[i].OccurredAt.Before(sortedProcesses[j].OccurredAt)
	})

	versionOf := func(process *emrprocess.ProcessRecord) (string, int, string) {
		snapshotID := ""
		if process.AfterSnapshotID != nil {
			snapshotID = *process.AfterSnapshotID
		} else if process.ActionSnapshotID != nil {
			snapshotID = *process.ActionSnapshotID
		}
		if snapshot := snapshotByID[snapshotID]; snapshot != nil {
			return fmt.Sprintf("版本 %d", snapshot.SnapshotNo), snapshot.RevisionNo, snapshot.ID
		}
		return "工作稿", 0, snapshotID
	}

	logHeaders := []string{"时间", "操作", "操作人", "结果", "失败原因", "状态变化", "版本号", "修订号", "快照ID", "备注", "病历号", "患者", "患者ID", "就诊号", "医生", "科室"}
	logRows := make([][]any, 0, len(sortedProcesses))
	for _, process := range sortedProcesses {
		version, revision, snapshotID := versionOf(process)
		revisionText := any("")
		if revision > 0 {
			revisionText = revision
		}
		patientID := any("")
		if record.PatientID != nil {
			patientID = *record.PatientID
		}
		logRows = append(logRows, []any{
			process.OccurredAt.Format("2006-01-02 15:04:05"), process.ActionType, process.ActorName, process.ActionResult,
			process.FailureReason, auditStatusPair(process.BeforeStatus, process.AfterStatus), version, revisionText, snapshotID,
			process.ActionNote, record.ID, patientName, patientID, record.EncounterID, doctorName, department,
		})
	}

	versionHeaders := []string{"版本号", "修订号", "生成时间", "形成人", "快照ID", "患者快照(JSON)", "病历正文(JSON)"}
	sortedSnapshots := append([]*Snapshot(nil), snapshots...)
	sort.SliceStable(sortedSnapshots, func(i, j int) bool { return sortedSnapshots[i].SnapshotNo < sortedSnapshots[j].SnapshotNo })
	versionRows := make([][]any, 0, len(sortedSnapshots))
	for _, snapshot := range sortedSnapshots {
		formedBy := actorNames[snapshot.FormedBy]
		if formedBy == "" {
			formedBy = fmt.Sprintf("用户 %d", snapshot.FormedBy)
		}
		versionRows = append(versionRows, []any{
			fmt.Sprintf("版本 %d", snapshot.SnapshotNo), snapshot.RevisionNo, snapshot.FormedAt.Format("2006-01-02 15:04:05"),
			formedBy, snapshot.ID, auditJSON(snapshot.PatientSnapshot), auditJSON(snapshot.Content),
		})
	}

	changeHeaders := []string{"事件ID", "时间", "操作", "操作人", "栏位", "变化类型", "原内容", "新内容", "说明"}
	changeRows := make([][]any, 0, len(sortedProcesses))
	for _, process := range sortedProcesses {
		if len(process.ContentChanges) == 0 {
			changeRows = append(changeRows, []any{process.ID, process.OccurredAt.Format("2006-01-02 15:04:05"), process.ActionType, process.ActorName, "", "", "", "", process.ActionNote})
			continue
		}
		for _, raw := range process.ContentChanges {
			change, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			code := auditString(change["section_code"])
			changeRows = append(changeRows, []any{
				process.ID, process.OccurredAt.Format("2006-01-02 15:04:05"), process.ActionType, process.ActorName,
				auditFieldLabel(code), auditString(change["change_type"]), auditString(change["before"]), auditString(change["after"]), process.ActionNote,
			})
		}
	}

	if err := writeAuditSheet(file, logsSheet, logHeaders, logRows, []float64{20, 14, 12, 8, 24, 20, 10, 8, 38, 26, 40, 16, 10, 12, 12, 20}); err != nil {
		return nil, err
	}
	if err := writeAuditSheet(file, versionsSheet, versionHeaders, versionRows, []float64{10, 8, 20, 12, 38, 48, 80}); err != nil {
		return nil, err
	}
	if err := writeAuditSheet(file, changesSheet, changeHeaders, changeRows, []float64{38, 20, 14, 12, 16, 10, 40, 40, 26}); err != nil {
		return nil, err
	}
	file.SetActiveSheet(0)
	return file, nil
}

func writeAuditSheet(file *excelize.File, sheet string, headers []string, rows [][]any, widths []float64) error {
	for index, header := range headers {
		cell, _ := excelize.CoordinatesToCellName(index+1, 1)
		if err := file.SetCellValue(sheet, cell, header); err != nil {
			return err
		}
	}
	for rowIndex, row := range rows {
		for columnIndex, value := range row {
			cell, _ := excelize.CoordinatesToCellName(columnIndex+1, rowIndex+2)
			if err := file.SetCellValue(sheet, cell, value); err != nil {
				return err
			}
		}
	}
	headerStyle, err := file.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "FFFFFF"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"1F4E79"}, Pattern: 1},
		Alignment: &excelize.Alignment{Vertical: "center", WrapText: true},
	})
	if err != nil {
		return err
	}
	bodyStyle, err := file.NewStyle(&excelize.Style{Alignment: &excelize.Alignment{Vertical: "top", WrapText: true}})
	if err != nil {
		return err
	}
	lastColumn, _ := excelize.ColumnNumberToName(len(headers))
	if err := file.SetCellStyle(sheet, "A1", lastColumn+"1", headerStyle); err != nil {
		return err
	}
	if len(rows) > 0 {
		if err := file.SetCellStyle(sheet, "A2", fmt.Sprintf("%s%d", lastColumn, len(rows)+1), bodyStyle); err != nil {
			return err
		}
	}
	if err := file.SetRowHeight(sheet, 1, 24); err != nil {
		return err
	}
	for index, width := range widths {
		column, _ := excelize.ColumnNumberToName(index + 1)
		if err := file.SetColWidth(sheet, column, column, width); err != nil {
			return err
		}
	}
	if err := file.SetPanes(sheet, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"}); err != nil {
		return err
	}
	if len(rows) > 0 {
		if err := file.AutoFilter(sheet, fmt.Sprintf("A1:%s1", lastColumn), nil); err != nil {
			return err
		}
	}
	return nil
}

// auditExportFilename 生成稳定的中文文件名（含患者与病历号前缀）。
func auditExportFilename(record *Record) string {
	patient := strings.TrimSpace(auditString(record.PatientSnapshot["name"]))
	if patient == "" {
		patient = "未命名患者"
	}
	shortID := record.ID
	if len(shortID) > 8 {
		shortID = shortID[:8]
	}
	return fmt.Sprintf("病历留痕日志-%s-%s.xlsx", patient, shortID)
}
