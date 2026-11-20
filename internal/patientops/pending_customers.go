package patientops

import (
	"context"
	"fmt"
	"strings"

	"github.com/freeasyman/lingce-api/internal/customercontact"
)

// PendingCustomerItem 是客户中心「待处理」页签的一行：一个客户 + 他当前最早的那条待办。
// 分页单位是「客户」而不是「任务」，所以必须在库里按客户分组后再分页，
// 不能像以前那样把全部待办拉回浏览器再分组切片。
type PendingCustomerItem struct {
	CustomerID    int64                     `json:"customer_id"`
	CustomerName  string                    `json:"customer_name"`
	CustomerPhone string                    `json:"customer_phone,omitempty"`
	Contacts      []customercontact.Contact `json:"contacts,omitempty"`
	PendingCount  int                       `json:"pending_count"`
	Task          PatientTaskResponse       `json:"task"`
}

// pendingTaskText 是队列归类用的文本，与前端原 taskMatchesQueue 拼的字段一致。
const pendingTaskText = `(COALESCE(t.task_type,'')||COALESCE(t.goal,'')||COALESCE(t.action,'')||COALESCE(t.content,''))`

// pendingTaskBucket 的分支顺序必须与前端原实现的 if/else 顺序一致：
// 先命中哪个队列就归哪个，都不命中则不属于任何队列（只出现在「全部待处理」）。
const pendingTaskBucket = `CASE
		WHEN ` + pendingTaskText + ` ~ '咨询|承接|回复|破冰' THEN '待承接咨询'
		WHEN ` + pendingTaskText + ` ~ '预约|到院|改期' THEN '待推进预约'
		WHEN ` + pendingTaskText + ` ~ '问题|回答|顾虑|决策' THEN '待确认问题'
		WHEN ` + pendingTaskText + ` ~ '复诊|服务|沉睡|唤醒' THEN '待恢复服务'
		WHEN ` + pendingTaskText + ` ~ '会员|套餐|权益|储值' THEN '待使用权益'
		ELSE ''
	END`

var pendingQueueNames = []string{"待承接咨询", "待推进预约", "待确认问题", "待恢复服务", "待使用权益"}

const pendingOwnerFilterEmployee = `(t.employee_id=$2 OR t.employee_id IN (SELECT id FROM employees WHERE tenant_id=$1 AND deleted_at IS NOT NULL))`

// ListPendingCustomers 返回「有待处理事项的客户」的一页（按客户分组），
// 以及客户总数和各队列的客户数。可见范围与 ListPatientTasks 完全一致。
func (s *Service) ListPendingCustomers(ctx context.Context, tenantID, employeeID int64, page, pageSize int, allEmployees bool, queue, query string) ([]PendingCustomerItem, int64, map[string]int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	ownerFilter := pendingOwnerFilterEmployee
	if allEmployees {
		ownerFilter = "($2::bigint IS NOT NULL)"
	}

	baseArgs := []any{tenantID, employeeID}
	searchFilter := ""
	if q := strings.TrimSpace(query); q != "" {
		baseArgs = append(baseArgs, "%"+q+"%")
		n := len(baseArgs)
		// 搜索覆盖客户姓名/电话 + 待办内容；task_type 也纳入，否则「预约推进」这种
		// 列表上肉眼可见的内容反而搜不到。
		searchFilter = fmt.Sprintf(` AND (c.name ILIKE $%[1]d OR COALESCE(c.phone,'') ILIKE $%[1]d OR COALESCE(t.task_type,'') ILIKE $%[1]d OR COALESCE(t.goal,'') ILIKE $%[1]d OR COALESCE(t.content,'') ILIKE $%[1]d OR COALESCE(t.action,'') ILIKE $%[1]d)`, n)
	}
	whereFilter := `t.tenant_id=$1 AND ` + ownerFilter + ` AND t.status NOT IN ('已完成','已取消')` + searchFilter

	fromClause := `
		FROM patient_operation_tasks t
		JOIN customers c ON c.id=t.customer_id AND c.tenant_id=t.tenant_id
		LEFT JOIN employees emp ON emp.id=t.employee_id
		LEFT JOIN products p ON p.id=t.product_id
		WHERE ` + whereFilter

	queue = strings.TrimSpace(queue)
	if queue == "全部待处理" {
		queue = ""
	}

	// 1) 当前队列下的客户总数 + 各队列的客户数（页签计数口径与旧前端一致：不受当前队列影响）
	countArgs := append(append([]any{}, baseArgs...), queue)
	queueParam := len(countArgs)
	countQuery := fmt.Sprintf(`WITH filtered AS (
			SELECT t.customer_id, %s AS bucket%s
		)
		SELECT count(DISTINCT customer_id) FILTER (WHERE $%d::text = '' OR bucket = $%d),
		       count(DISTINCT customer_id) FILTER (WHERE bucket='待承接咨询'),
		       count(DISTINCT customer_id) FILTER (WHERE bucket='待推进预约'),
		       count(DISTINCT customer_id) FILTER (WHERE bucket='待确认问题'),
		       count(DISTINCT customer_id) FILTER (WHERE bucket='待恢复服务'),
		       count(DISTINCT customer_id) FILTER (WHERE bucket='待使用权益'),
		       count(DISTINCT customer_id)
		FROM filtered`, pendingTaskBucket, fromClause, queueParam, queueParam)

	var total, allPending int64
	bucketCounts := make([]int64, len(pendingQueueNames))
	if err := s.pool.QueryRow(ctx, countQuery, countArgs...).Scan(
		&total, &bucketCounts[0], &bucketCounts[1], &bucketCounts[2], &bucketCounts[3], &bucketCounts[4], &allPending,
	); err != nil {
		return nil, 0, nil, fmt.Errorf("count pending customers: %w", err)
	}
	counts := map[string]int64{"全部待处理": allPending}
	for i, name := range pendingQueueNames {
		counts[name] = bucketCounts[i]
	}

	// 2) 当前页的客户 + 每户最早的一条待办
	pageArgs := append(append([]any{}, baseArgs...), queue)
	queueIdx := len(pageArgs)
	limitIdx := queueIdx + 1
	offsetIdx := queueIdx + 2
	pageArgs = append(pageArgs, pageSize, (page-1)*pageSize)

	pageQuery := fmt.Sprintf(`WITH filtered AS (
			SELECT %s, %s AS bucket%s
		), bucketed AS (
			SELECT *, count(*) OVER (PARTITION BY customer_id)::int AS pending_count
			FROM filtered
			WHERE $%d::text = '' OR bucket = $%d
		), ranked AS (
			SELECT *, row_number() OVER (PARTITION BY customer_id ORDER BY due_at, created_at DESC, id DESC) AS rn
			FROM bucketed
		)
		SELECT * FROM ranked WHERE rn = 1
		ORDER BY due_at, customer_id
		LIMIT $%d OFFSET $%d`,
		patientTaskSelect, pendingTaskBucket, fromClause, queueIdx, queueIdx, limitIdx, offsetIdx)

	rows, err := s.pool.Query(ctx, pageQuery, pageArgs...)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("list pending customers: %w", err)
	}
	defer rows.Close()

	items := []PendingCustomerItem{}
	tasks := []PatientTaskResponse{}
	for rows.Next() {
		var bucket string
		var pendingCount int
		var rank int64
		task, scanErr := scanPatientTask(rows, &bucket, &pendingCount, &rank)
		if scanErr != nil {
			return nil, 0, nil, scanErr
		}
		tasks = append(tasks, task)
		items = append(items, PendingCustomerItem{
			CustomerID:    task.CustomerID,
			CustomerName:  task.CustomerName,
			CustomerPhone: task.CustomerPhone,
			PendingCount:  pendingCount,
			Task:          task,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, 0, nil, err
	}
	rows.Close()

	if err := s.populateTaskContacts(ctx, tenantID, tasks); err != nil {
		return nil, 0, nil, err
	}
	for i := range items {
		items[i].Contacts = tasks[i].Contacts
		items[i].Task = tasks[i]
	}
	return items, total, counts, nil
}
