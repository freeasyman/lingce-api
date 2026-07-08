# 迁移清单

> 本文档是 lingce-api 的完整迁移清单，列出所有需要迁移的 API 端点和数据库表。
> 每个端点标注了优先级（P0=必须/P1=重要/P2=可延后）和来源文件。

## 总览

| 模块 | 端点数 | 数据表数 | Python 来源文件 |
|------|--------|---------|----------------|
| auth | 9 | 7 | auth.py |
| organization | 43 | 13 | organization.py, institutions.py, departments.py, doctors.py, patients.py |
| rbac | 44 | 11 | rbac.py, inst_rbac.py |
| sysconfig | 20 | 10 | config.py (部分), tenant_feature.py |
| recording | 77 | 16 | recordings.py, medical_recordings.py, recording_tasks.py, recording_dashboard.py, employee_diagnosis.py, morning_meeting.py, team_ability.py, employee_growth.py, recording_prompts.py |
| badge | 46 | 11 | badge_control.py, smart_badge.py |
| content | 74 | 8 | content.py, content_seeds.py, prompt_templates.py, content_prompt_templates.py |
| customer | 35 | 10 | customers.py, customer_groups.py |
| support | 36 | 6 | notifications.py, logs.py, llm_config.py, llm_records.py, llm_cost.py, metadata.py, data_browser.py, visits.py |
| **合计** | **384** | **~60** | |

---

## 一、auth 模块（9 端点）

> Python 来源：`app/api/auth.py`

| # | 方法 | 路径 | 说明 | 优先级 |
|---|------|------|------|--------|
| 1 | GET | `/api/v1/auth/captcha` | 获取图形验证码 | P0 |
| 2 | POST | `/api/v1/auth/login` | 运营管理员登录 | P0 |
| 3 | POST | `/api/v1/auth/login/institution` | 机构员工登录 | P0 |
| 4 | POST | `/api/v1/auth/login/employee` | 员工移动端登录 | P0 |
| 5 | POST | `/api/v1/auth/login/mobile` | 移动端专用登录（会话隔离） | P0 |
| 6 | GET | `/api/v1/auth/me` | 获取当前用户信息 | P0 |
| 7 | POST | `/api/v1/auth/change-password` | 修改密码 | P1 |
| 8 | POST | `/api/v1/auth/mobile/sms/send` | 发送短信验证码 | P1 |
| 9 | POST | `/api/v1/auth/mobile/sms/login` | 短信验证码登录 | P1 |

### 数据表

| 表名 | 操作 | 说明 |
|------|------|------|
| operations_admins | R/W | 运营管理员账户 |
| employees | R/W | 机构员工账户 |
| tenants | R | 租户信息 |
| sms_login_codes | R/W/D | 短信验证码 |
| operation_logs | W | 登录日志 |
| inst_employee_roles | R | 员工角色查询 |
| tenant_subscriptions | R | 租户订阅有效期验证 |

### 外部依赖
- 阿里云 DySMS API（短信发送）

---

## 二、organization 模块（43 端点）

### 2.1 组织架构 — organization.py（11 端点）

| # | 方法 | 路径 | 说明 | 优先级 |
|---|------|------|------|--------|
| 1 | GET | `/api/v1/organization/medical-specialties` | 医学专科目录 | P1 |
| 2 | GET | `/api/v1/organization/departments` | 部门列表 | P0 |
| 3 | GET | `/api/v1/organization/employees` | 员工列表 | P0 |
| 4 | GET | `/api/v1/organization/employees/{id}/assistants` | 医助绑定查询 | P1 |
| 5 | PUT | `/api/v1/organization/employees/{id}/assistants` | 医助绑定更新 | P1 |
| 6 | POST | `/api/v1/organization/employees` | 创建员工 | P0 |
| 7 | PUT | `/api/v1/organization/employees/{id}` | 更新员工 | P0 |
| 8 | DELETE | `/api/v1/organization/employees/{id}` | 删除员工 | P1 |
| 9 | POST | `/api/v1/organization/departments` | 创建部门 | P0 |
| 10 | PUT | `/api/v1/organization/departments/{id}` | 更新部门 | P0 |
| 11 | DELETE | `/api/v1/organization/departments/{id}` | 删除部门 | P1 |

### 2.2 机构管理 — institutions.py（6 端点）

| # | 方法 | 路径 | 说明 | 优先级 |
|---|------|------|------|--------|
| 12 | GET | `/api/v1/institutions/statistics` | 机构统计 | P1 |
| 13 | GET | `/api/v1/institutions` | 机构列表 | P0 |
| 14 | GET | `/api/v1/institutions/{id}` | 机构详情 | P0 |
| 15 | POST | `/api/v1/institutions` | 创建机构 | P0 |
| 16 | PUT | `/api/v1/institutions/{id}` | 更新机构 | P0 |
| 17 | DELETE | `/api/v1/institutions/{id}` | 删除机构 | P1 |

### 2.3 科室管理 — departments.py（8 端点）

| # | 方法 | 路径 | 说明 | 优先级 |
|---|------|------|------|--------|
| 18 | GET | `/api/v1/departments/health` | 健康检查 | P2 |
| 19 | GET | `/api/v1/departments` | 科室列表 | P0 |
| 20 | GET | `/api/v1/departments/{id}` | 科室详情 | P0 |
| 21 | POST | `/api/v1/departments` | 创建科室 | P0 |
| 22 | PUT | `/api/v1/departments/{id}` | 更新科室 | P0 |
| 23 | DELETE | `/api/v1/departments/{id}` | 删除科室 | P1 |
| 24 | POST | `/api/v1/departments/sync-from-visits` | 从就诊同步科室 | P1 |
| 25 | GET | `/api/v1/departments/{id}/performance` | 科室业绩统计 | P1 |

### 2.4 医生管理 — doctors.py（11 端点）

| # | 方法 | 路径 | 说明 | 优先级 |
|---|------|------|------|--------|
| 26 | GET | `/api/v1/doctors/health` | 健康检查 | P2 |
| 27 | GET | `/api/v1/doctors` | 医生列表 | P0 |
| 28 | GET | `/api/v1/doctors/{id}` | 医生详情 | P0 |
| 29 | POST | `/api/v1/doctors` | 创建医生 | P0 |
| 30 | PUT | `/api/v1/doctors/{id}` | 更新医生 | P0 |
| 31 | DELETE | `/api/v1/doctors/{id}` | 删除医生 | P1 |
| 32 | POST | `/api/v1/doctors/sync-from-visits` | 从就诊同步医生 | P1 |
| 33 | GET | `/api/v1/doctors/{id}/performance` | 医生业绩统计 | P1 |
| 34 | GET | `/api/v1/doctors/performance/summary` | 医生业绩汇总 | P1 |
| 35 | GET | `/api/v1/doctors/{id}/employees` | 医生映射员工 | P1 |
| 36 | POST | `/api/v1/doctors/{id}/employees` | 更新医生员工映射 | P1 |

### 2.5 患者管理 — patients.py（7 端点）

| # | 方法 | 路径 | 说明 | 优先级 |
|---|------|------|------|--------|
| 37 | GET | `/api/v1/patients` | 患者列表 | P0 |
| 38 | POST | `/api/v1/patients/sync-from-visits` | 从就诊同步患者 | P1 |
| 39 | GET | `/api/v1/patients/{id}` | 患者详情 | P0 |
| 40 | POST | `/api/v1/patients` | 创建患者 | P0 |
| 41 | PUT | `/api/v1/patients/{id}` | 更新患者 | P0 |
| 42 | DELETE | `/api/v1/patients/{id}` | 删除患者 | P1 |
| 43 | GET | `/api/v1/patients/{id}/360` | 患者 360 度视图 | P1 |

### 数据表

| 表名 | 说明 |
|------|------|
| tenants | 租户/机构（复用为机构表） |
| employees | 员工 |
| departments | 内部部门 |
| his_departments | HIS 科室 |
| doctors | 医生 |
| patients | 患者 |
| op_visits | 门诊就诊记录 |
| user_masters | 用户主档案 |
| inst_employee_roles | 员工角色 |
| roles | 角色定义 |
| employee_doctor_mappings | 员工-医生映射 |
| employee_assistant_assignments | 医助绑定 |
| inst_department_roles | 部门默认角色 |

---

## 三、rbac 模块（44 端点）

### 3.1 运营端 RBAC — rbac.py（23 端点）

| # | 方法 | 路径 | 说明 | 优先级 |
|---|------|------|------|--------|
| 1 | GET | `/api/v1/rbac/roles` | 角色列表 | P0 |
| 2 | POST | `/api/v1/rbac/roles` | 创建角色 | P0 |
| 3 | GET | `/api/v1/rbac/roles/{id}` | 角色详情 | P0 |
| 4 | PUT | `/api/v1/rbac/roles/{id}` | 更新角色 | P0 |
| 5 | DELETE | `/api/v1/rbac/roles/{id}` | 删除角色 | P1 |
| 6 | POST | `/api/v1/rbac/roles/{id}/permissions` | 分配权限 | P0 |
| 7 | DELETE | `/api/v1/rbac/roles/{id}/permissions` | 移除权限 | P1 |
| 8 | GET | `/api/v1/rbac/roles/{id}/permissions` | 查询权限 | P0 |
| 9 | GET | `/api/v1/rbac/menus` | 菜单列表 | P0 |
| 10 | POST | `/api/v1/rbac/menus` | 创建菜单 | P0 |
| 11 | GET | `/api/v1/rbac/menus/{id}` | 菜单详情 | P0 |
| 12 | PUT | `/api/v1/rbac/menus/{id}` | 更新菜单 | P0 |
| 13 | DELETE | `/api/v1/rbac/menus/{id}` | 删除菜单 | P1 |
| 14 | PUT | `/api/v1/rbac/menus/sort` | 菜单排序 | P1 |
| 15 | GET | `/api/v1/rbac/menus/tree` | 菜单树 | P0 |
| 16 | GET | `/api/v1/rbac/admins` | 管理员列表 | P0 |
| 17 | POST | `/api/v1/rbac/admins` | 创建管理员 | P0 |
| 18 | GET | `/api/v1/rbac/admins/{id}` | 管理员详情 | P0 |
| 19 | PUT | `/api/v1/rbac/admins/{id}` | 更新管理员 | P0 |
| 20 | DELETE | `/api/v1/rbac/admins/{id}` | 删除管理员 | P1 |
| 21 | PUT | `/api/v1/rbac/admins/{id}/reset-password` | 重置密码 | P1 |
| 22 | GET | `/api/v1/rbac/roles/{id}/menus` | 角色菜单列表 | P0 |
| 23 | PUT | `/api/v1/rbac/roles/{id}/menus` | 更新角色菜单 | P0 |

### 3.2 机构端 RBAC — inst_rbac.py（21 端点）

| # | 方法 | 路径 | 说明 | 优先级 |
|---|------|------|------|--------|
| 1 | GET | `/api/v1/institution/rbac/roles` | 机构角色列表 | P0 |
| 2 | POST | `/api/v1/institution/rbac/roles` | 创建机构角色 | P0 |
| 3 | GET | `/api/v1/institution/rbac/roles/{id}` | 机构角色详情 | P0 |
| 4 | PUT | `/api/v1/institution/rbac/roles/{id}` | 更新机构角色 | P0 |
| 5 | DELETE | `/api/v1/institution/rbac/roles/{id}` | 删除机构角色 | P1 |
| 6 | GET | `/api/v1/institution/rbac/menus` | 机构菜单列表 | P0 |
| 7 | POST | `/api/v1/institution/rbac/menus` | 创建机构菜单 | P0 |
| 8 | GET | `/api/v1/institution/rbac/menus/{id}` | 机构菜单详情 | P0 |
| 9 | PUT | `/api/v1/institution/rbac/menus/{id}` | 更新机构菜单 | P0 |
| 10 | DELETE | `/api/v1/institution/rbac/menus/{id}` | 删除机构菜单 | P1 |
| 11 | POST | `/api/v1/institution/rbac/roles/{id}/permissions` | 分配权限 | P0 |
| 12 | DELETE | `/api/v1/institution/rbac/roles/{id}/permissions` | 移除权限 | P1 |
| 13 | GET | `/api/v1/institution/rbac/roles/{id}/permissions` | 查询权限 | P0 |
| 14 | GET | `/api/v1/institution/rbac/employees/{id}/role` | 查询员工角色 | P0 |
| 15 | PUT | `/api/v1/institution/rbac/employees/{id}/role` | 设置员工角色 | P0 |
| 16 | DELETE | `/api/v1/institution/rbac/employees/{id}/role` | 移除员工角色 | P1 |
| 17 | GET | `/api/v1/institution/rbac/departments/{id}/role` | 查询部门默认角色 | P1 |
| 18 | PUT | `/api/v1/institution/rbac/departments/{id}/role` | 设置部门默认角色 | P1 |
| 19 | DELETE | `/api/v1/institution/rbac/departments/{id}/role` | 移除部门默认角色 | P1 |
| 20 | GET | `/api/v1/institution/rbac/roles/{id}/menus` | 角色菜单列表 | P0 |
| 21 | PUT | `/api/v1/institution/rbac/roles/{id}/menus` | 更新角色菜单 | P0 |

### 数据表

| 表名 | 说明 |
|------|------|
| ops_roles | 运营端角色 |
| ops_menus | 运营端菜单 |
| ops_role_menus | 运营端角色-菜单关联 |
| ops_admin_roles | 管理员-角色关联 |
| ops_casbin_rule | 运营端 Casbin 策略 |
| inst_roles | 机构端角色 |
| inst_menus | 机构端菜单 |
| inst_role_menus | 机构端角色-菜单关联 |
| inst_department_roles | 部门默认角色 |
| inst_employee_roles | 员工角色 |
| inst_casbin_rule | 机构端 Casbin 策略 |

---

## 四、sysconfig 模块（20 端点）

> Python 来源：`app/api/config.py`（部分）、`app/api/tenant_feature.py`
> 注：config.py 中与旧任务系统（tasks 表）相关的端点不迁移。

### 4.1 标准变量 — config.py（5 端点）

| # | 方法 | 路径 | 说明 | 优先级 |
|---|------|------|------|--------|
| 1 | GET | `/api/v1/config/standard-variables` | 标准变量列表 | P1 |
| 2 | POST | `/api/v1/config/standard-variables` | 创建标准变量 | P1 |
| 3 | GET | `/api/v1/config/standard-variables/{id}` | 标准变量详情 | P1 |
| 4 | PUT | `/api/v1/config/standard-variables/{id}` | 更新标准变量 | P1 |
| 5 | DELETE | `/api/v1/config/standard-variables/{id}` | 删除标准变量 | P2 |

### 4.2 租户管理 — config.py（6 端点）

| # | 方法 | 路径 | 说明 | 优先级 |
|---|------|------|------|--------|
| 6 | GET | `/api/v1/config/tenants` | 租户列表 | P0 |
| 7 | POST | `/api/v1/config/tenants` | 创建租户 | P0 |
| 8 | GET | `/api/v1/config/tenants/{id}` | 租户详情 | P0 |
| 9 | PUT | `/api/v1/config/tenants/{id}` | 更新租户 | P0 |
| 10 | DELETE | `/api/v1/config/tenants/{id}` | 删除租户 | P1 |
| 11 | GET | `/api/v1/config/tenants/test` | 测试端点（无认证） | P2 |

### 4.3 订阅管理 — config.py（7 端点）

| # | 方法 | 路径 | 说明 | 优先级 |
|---|------|------|------|--------|
| 12 | GET | `/api/v1/config/subscription/plans` | 订阅计划列表 | P0 |
| 13 | POST | `/api/v1/config/subscription/plans` | 创建订阅计划 | P0 |
| 14 | PUT | `/api/v1/config/subscription/plans/{id}` | 更新订阅计划 | P1 |
| 15 | GET | `/api/v1/config/tenants/{id}/subscription` | 租户订阅状态 | P0 |
| 16 | POST | `/api/v1/config/tenants/{id}/subscription/actions` | 订阅操作（续期/升级/暂停/取消） | P0 |
| 17 | GET | `/api/v1/config/tenants/{id}/subscription/events` | 订阅事件历史 | P1 |
| 18 | GET | `/api/v1/config/tenants/{id}/validity-logs` | 有效期变更日志 | P2 |

### 4.4 租户功能控制 — tenant_feature.py（9 端点）

> 注：路径前缀为 `/api/v1/config`

| # | 方法 | 路径 | 说明 | 优先级 |
|---|------|------|------|--------|
| 19 | GET | `/api/v1/config/tenant-feature-groups` | 功能分组列表 | P0 |
| 20 | POST | `/api/v1/config/tenant-feature-groups` | 创建功能分组 | P0 |
| 21 | PUT | `/api/v1/config/tenant-feature-groups/{id}` | 更新功能分组 | P0 |
| 22 | DELETE | `/api/v1/config/tenant-feature-groups/{id}` | 删除功能分组 | P1 |
| 23 | PUT | `/api/v1/config/tenants/{id}/feature-group` | 分配功能分组 | P0 |
| 24 | GET | `/api/v1/config/tenants/{id}/feature-overrides` | 功能覆盖规则 | P0 |
| 25 | PUT | `/api/v1/config/tenants/{id}/feature-overrides` | 更新覆盖规则 | P0 |
| 26 | GET | `/api/v1/config/tenants/{id}/effective-feature-policy` | 有效功能策略 | P0 |
| 27 | GET | `/api/v1/config/tenant-feature-options` | 可用功能选项 | P1 |

### 数据表

| 表名 | 说明 |
|------|------|
| standard_variables | 标准变量 |
| tenants | 租户 |
| tenant_subscriptions | 租户订阅 |
| tenant_subscription_plans | 订阅计划 |
| tenant_subscription_events | 订阅事件 |
| tenant_validity_change_logs | 有效期变更日志 |
| tenant_feature_groups | 功能分组 |
| tenant_feature_group_items | 分组项 |
| tenant_feature_overrides | 功能覆盖 |
| tenant_feature_change_logs | 功能变更日志 |

### 暂不迁移（旧任务系统相关，共 47 端点）

以下端点基于 `tasks` 表（旧链路），本次不迁移：
- 任务规则管理（7 端点）：`/api/v1/config/task-rules/*`
- 任务模板管理（2 端点）：`/api/v1/config/task-templates/*`
- 任务生成（4 端点）：`/api/v1/config/generate-tasks` 等
- 数据源/元数据（3 端点）：`/api/v1/config/data-sources/*`
- 数据映射（8 端点）：`/api/v1/config/mapping-configurations/*`
- 调度器管理（8 端点）：`/api/v1/config/scheduler/*`
- 仪表板统计（4 端点）：`/api/v1/config/dashboard/*`
- 定时任务管理（17 端点）：`/api/v1/scheduled-tasks/*`
- 任务提示词模板（3 端点）：`/api/v1/task-prompt-templates/*`
- 任务数据源（2 端点）：`/api/v1/task-rules/datasources/*`

> 注：数据映射（mapping-configurations）虽然在 config.py 中，但如果仅服务于旧任务系统，也暂不迁移。如果其他模块也使用，需要重新评估。

---

## 五、recording 模块（77 端点）

### 5.1 咨询师录音 — recordings.py（27 端点）

| # | 方法 | 路径 | 说明 | 优先级 |
|---|------|------|------|--------|
| 1 | GET | `/api/v1/recordings` | 录音列表 | P0 |
| 2 | GET | `/api/v1/recordings/stats/overview` | 统计概览 | P0 |
| 3 | GET | `/api/v1/recordings/stats/by-scene` | 按场景统计 | P1 |
| 4 | GET | `/api/v1/recordings/stats/by-source` | 按来源统计 | P1 |
| 5 | GET | `/api/v1/recordings/stats/by-tenant` | 按租户统计 | P1 |
| 6 | GET | `/api/v1/recordings/stats/duration-distribution` | 时长分布 | P1 |
| 7 | GET | `/api/v1/recordings/stats/daily` | 每日统计 | P1 |
| 8 | POST | `/api/v1/recordings/upload` | 上传录音 | P0 |
| 9 | GET | `/api/v1/recordings/{id}` | 录音详情 | P0 |
| 10 | PATCH | `/api/v1/recordings/{id}` | 更新录音 | P0 |
| 11 | GET | `/api/v1/recordings/{id}/play-url` | 播放 URL | P0 |
| 12 | GET | `/api/v1/recordings/{id}/file-test` | 测试播放 | P2 |
| 13 | POST | `/api/v1/recordings/{id}/transcribe` | 触发转写 | P0 |
| 14 | POST | `/api/v1/recordings/{id}/analyze` | 触发分析 | P0 |
| 15 | POST | `/api/v1/recordings/{id}/clean` | 触发清洗 | P0 |
| 16 | GET | `/api/v1/recordings/{id}/analysis` | 分析结果 | P0 |
| 17 | POST | `/api/v1/recordings/{id}/follow-up-tasks/dispatch` | 派发跟进任务 | P0 |
| 18 | POST | `/api/v1/recordings/{id}/analysis/feedback` | 分析反馈 | P1 |
| 19 | POST | `/api/v1/recordings/batch-transcribe` | 批量转写 | P1 |
| 20 | POST | `/api/v1/recordings/batch-delete` | 批量删除 | P1 |
| 21 | DELETE | `/api/v1/recordings/{id}` | 删除录音 | P1 |
| 22 | GET | `/api/v1/recordings/{id}/learning-recommendation` | 学习推荐 | P1 |
| 23 | POST | `/api/v1/recordings/{id}/confirm-action` | 确认跟进方式 | P0 |
| 24 | POST | `/api/v1/recordings/{id}/generate-opening-script` | 生成跟进话术 | P1 |
| 25 | POST | `/api/v1/recordings/{id}/generate-operations-plan` | 生成运营计划 | P1 |
| 26 | GET | `/api/v1/recordings/{id}/operations-plan-jobs/{job_id}` | 运营计划任务状态 | P1 |
| 27 | GET | `/api/v1/recordings/{id}/tasks` | 录音关联任务 | P0 |

### 5.2 医疗录音 — medical_recordings.py（19 端点）

| # | 方法 | 路径 | 说明 | 优先级 |
|---|------|------|------|--------|
| 1 | GET | `/api/v1/medical-recordings` | 医疗录音列表 | P0 |
| 2 | GET | `/api/v1/medical-recordings/quality-control` | 质检看板 | P0 |
| 3 | GET | `/api/v1/medical-recordings/doctor-ability` | 医生能力排行 | P0 |
| 4 | GET | `/api/v1/medical-recordings/doctor-ability/{employee_id}` | 医生能力详情 | P0 |
| 5 | GET | `/api/v1/medical-recordings/analysis` | 沟通分析 | P0 |
| 6 | GET | `/api/v1/medical-recordings/weekly-meeting` | 周会素材 | P1 |
| 7 | GET | `/api/v1/medical-recordings/weekly-summary` | 周总结 | P1 |
| 8 | GET | `/api/v1/medical-recordings/team-trends` | 团队趋势 | P1 |
| 9 | POST | `/api/v1/medical-recordings/{id}/best-practice` | 添加最佳实践 | P1 |
| 10 | DELETE | `/api/v1/medical-recordings/{id}/best-practice` | 删除最佳实践 | P1 |
| 11 | GET | `/api/v1/medical-recordings/best-practices` | 最佳实践库 | P1 |
| 12 | POST | `/api/v1/medical-recordings/{id}/mark-highlight` | 标记亮点 | P2 |
| 13 | GET | `/api/v1/medical-recordings/followup-generation-mode` | 跟进任务生成模式 | P0 |
| 14 | POST | `/api/v1/medical-recordings/{id}/follow-up-tasks/confirm` | 确认跟进任务 | P0 |
| 15 | POST | `/api/v1/medical-recordings/followup-generation-mode` | 更新生成模式 | P0 |
| 16 | GET | `/api/v1/medical-recordings/institution-rule-configs` | 机构规则配置列表 | P1 |
| 17 | POST | `/api/v1/medical-recordings/institution-rule-configs` | 创建规则配置 | P1 |
| 18 | PUT | `/api/v1/medical-recordings/institution-rule-configs/{id}` | 更新规则配置 | P1 |
| 19 | GET | `/api/v1/medical-recordings/recording-analysis/settings` | 分析设置 | P1 |

### 5.3 录音任务 — recording_tasks.py（13 端点）

| # | 方法 | 路径 | 说明 | 优先级 |
|---|------|------|------|--------|
| 1 | GET | `/api/v1/recording-tasks/stats` | 任务统计 | P0 |
| 2 | GET | `/api/v1/recording-tasks/daily-briefing` | 今日简报 | P0 |
| 3 | GET | `/api/v1/recording-tasks/my-tasks` | 我的任务 | P0 |
| 4 | GET | `/api/v1/recording-tasks` | 全部任务 | P0 |
| 5 | GET | `/api/v1/recording-tasks/recordings/{id}/tasks` | 录音的任务 | P0 |
| 6 | GET | `/api/v1/recording-tasks/employees` | 员工列表 | P1 |
| 7 | POST | `/api/v1/recording-tasks/assign` | 批量分配 | P0 |
| 8 | POST | `/api/v1/recording-tasks/{id}/complete` | 完成任务 | P0 |
| 9 | POST | `/api/v1/recording-tasks/{id}/cancel` | 取消任务 | P0 |
| 10 | GET | `/api/v1/recording-tasks/employee-partnerships` | 协作关系列表 | P1 |
| 11 | POST | `/api/v1/recording-tasks/employee-partnerships` | 创建协作关系 | P1 |
| 12 | DELETE | `/api/v1/recording-tasks/employee-partnerships/{id}` | 删除协作关系 | P1 |
| 13 | GET | `/api/v1/recording-tasks/{id}` | 任务详情 | P0 |

### 5.4 录音看板 — recording_dashboard.py（4 端点）

| # | 方法 | 路径 | 说明 | 优先级 |
|---|------|------|------|--------|
| 1 | GET | `/api/v1/recordings/dashboard/daily-report` | 经营日报 | P0 |
| 2 | GET | `/api/v1/recordings/dashboard/diagnosis` | 运营诊断 | P0 |
| 3 | PATCH | `/api/v1/recordings/dashboard/target` | 更新月度目标 | P1 |
| 4 | GET | `/api/v1/recordings/dashboard/funnel-detail` | 漏斗详情 | P1 |

### 5.5 分析看板（4 端点）

| # | 方法 | 路径 | 说明 | 来源文件 | 优先级 |
|---|------|------|------|---------|--------|
| 1 | GET | `/api/v1/recordings/dashboard/employee-diagnosis` | 员工诊断卡 | employee_diagnosis.py | P1 |
| 2 | GET | `/api/v1/recordings/dashboard/team-ability` | 团队能力分析 | team_ability.py | P1 |
| 3 | GET | `/api/v1/recordings/dashboard/morning-meeting` | 早会素材 | morning_meeting.py | P1 |
| 4 | GET | `/api/v1/recordings/dashboard/employee-growth` | 个人成长档案 | employee_growth.py | P1 |

### 5.6 录音提示词 — recording_prompts.py（10 端点）

| # | 方法 | 路径 | 说明 | 优先级 |
|---|------|------|------|--------|
| 1 | GET | `/api/v1/recording-prompts` | 提示词列表 | P0 |
| 2 | POST | `/api/v1/recording-prompts` | 创建提示词 | P0 |
| 3 | GET | `/api/v1/recording-prompts/codes/{code}` | 提示词详情 | P0 |
| 4 | PUT | `/api/v1/recording-prompts/codes/{code}` | 更新提示词 | P0 |
| 5 | DELETE | `/api/v1/recording-prompts/codes/{code}` | 删除提示词 | P1 |
| 6 | POST | `/api/v1/recording-prompts/codes/{code}/test` | 测试提示词 | P1 |
| 7 | GET | `/api/v1/recording-prompts/tenant-configs` | 租户配置列表 | P0 |
| 8 | POST | `/api/v1/recording-prompts/tenant-configs` | 创建租户配置 | P0 |
| 9 | PUT | `/api/v1/recording-prompts/tenant-configs/{id}` | 更新租户配置 | P0 |
| 10 | DELETE | `/api/v1/recording-prompts/tenant-configs/{id}` | 删除租户配置 | P1 |

### 数据表

| 表名 | 说明 |
|------|------|
| recordings | 录音主表 |
| recording_tasks | 录音任务（新链路） |
| recording_analysis_results | 分析结果 |
| recording_emr_drafts | EMR 草稿 |
| recording_best_practices | 最佳实践 |
| recording_analysis_prompts | 分析提示词 |
| recording_analysis_tenant_configs | 租户提示词配置 |
| recording_institution_rule_configs | 机构规则配置 |
| recording_content_seeds | 内容素材 |
| recording_performance_events | 性能事件 |
| recording_segue_scores | SEGUE 评分 |
| recording_route_results | 路由结果 |
| employee_partnerships | 员工协作关系 |
| concern_clusters | 关注聚类 |
| recording_operations_plan_jobs | 运营计划任务 |
| customer_interactions | 客户互动（写入） |

### 外部依赖
- recording-worker（POST /v1/jobs 触发转写/分析）
- llm-gateway（话术生成、运营计划）
- 阿里云 OSS（录音文件存储）

---

## 六、badge 模块（46 端点）

### 6.1 设备生命周期管理 — badge_control.py（33 端点）

| # | 方法 | 路径 | 说明 | 优先级 |
|---|------|------|------|--------|
| 1 | POST | `/api/v1/badge-control/acceptance/validate` | 验收导入验证 | P0 |
| 2 | POST | `/api/v1/badge-control/acceptance/import` | 验收导入 | P0 |
| 3 | POST | `/api/v1/badge-control/acceptance/vendor-check` | 厂商核验 | P0 |
| 4 | POST | `/api/v1/badge-control/inspection/device/{device_id}` | 单台检测 | P0 |
| 5 | POST | `/api/v1/badge-control/inspection/batch` | 批量检测 | P0 |
| 6 | GET | `/api/v1/badge-control/inspection/devices` | 检测设备列表 | P0 |
| 7 | GET | `/api/v1/badge-control/inspection/device/{device_id}/live-status` | 设备实时状态 | P1 |
| 8 | POST | `/api/v1/badge-control/inspection/device/{device_id}/recording-test` | 录音测试 | P1 |
| 13 | POST | `/api/v1/badge-control/tickets/submit` | 提交工单 | P0 |
| 14 | POST | `/api/v1/badge-control/tickets/submit-by-device` | 按设备提交工单 | P0 |
| 15 | GET | `/api/v1/badge-control/tickets/my` | 我的工单 | P0 |
| 16 | GET | `/api/v1/badge-control/tickets` | 工单列表 | P0 |
| 17 | POST | `/api/v1/badge-control/tickets/{ticket_id}/review` | 审核工单 | P0 |
| 18 | POST | `/api/v1/badge-control/tickets/{ticket_id}/execute` | 执行工单 | P0 |
| 19 | GET | `/api/v1/badge-control/devices` | 设备台账 | P0 |
| 20 | GET | `/api/v1/badge-control/devices/{device_id}/lifecycle` | 设备生命周期日志 | P1 |
| 21 | GET | `/api/v1/badge-control/manufacturers` | 厂商列表 | P0 |
| 22 | PUT | `/api/v1/badge-control/manufacturers/{manufacturer_code}/config` | 更新厂商配置 | P1 |
| 23 | GET | `/api/v1/badge-control/dashboard/summary` | 仪表板概览 | P0 |
| 24 | POST | `/api/v1/badge-control/vendor-pool/sync-devices` | 同步厂家设备池 | P0 |
| 25 | POST | `/api/v1/badge-control/vendor-pool/sync-and-diff` | 同步并对比差异 | P0 |
| 26 | GET | `/api/v1/badge-control/vendor-pool/diff` | 查看差异 | P0 |
| 27 | GET | `/api/v1/badge-control/vendor-pool/sync-batches` | 同步批次列表 | P1 |
| 28 | GET | `/api/v1/badge-control/vendor-pool/sync-batches/{id}/items` | 批次详情 | P1 |
| 29 | POST | `/api/v1/badge-control/vendor-pool/sync-batches/{id}/rollback-drafts` | 回滚草稿 | P1 |
| 30 | POST | `/api/v1/badge-control/vendor-pool/actions/create-acceptance-drafts` | 创建验收草稿 | P0 |
| 31 | POST | `/api/v1/badge-control/vendor-pool/actions/mark-pending-assignment` | 标记待分配 | P1 |
| 32 | POST | `/api/v1/badge-control/vendor-pool/actions/create-exception-tickets` | 创建异常工单 | P1 |
| 33 | GET | `/api/v1/badge-control/tenant-employees` | 租户员工列表 | P1 |

### 6.2 智能工牌 — smart_badge.py（13 端点）

| # | 方法 | 路径 | 说明 | 优先级 |
|---|------|------|------|--------|
| 1 | GET | `/api/v1/smart-badge/tenant/devices` | 租户设备列表 | P0 |
| 2 | GET | `/api/v1/smart-badge/tenant/devices/overview` | 租户设备概览 | P0 |
| 3 | GET | `/api/v1/smart-badge/tenant/devices/{device_no}/history` | 设备历史 | P1 |
| 4 | GET | `/api/v1/smart-badge/tenant/recording-control/devices` | 录音控制设备列表 | P0 |
| 5 | POST | `/api/v1/smart-badge/tenant/devices/{device_no}/recording/start` | 启动录音 | P0 |
| 6 | POST | `/api/v1/smart-badge/tenant/devices/{device_no}/recording/stop` | 停止录音 | P0 |
| 7 | GET | `/api/v1/smart-badge/tenant/recording-control/logs` | 录音控制日志 | P1 |
| 8 | POST | `/api/v1/smart-badge/callback/developer` | 开发者回调 | P0 |
| 9 | POST | `/api/v1/smart-badge/callback/audio` | 音频回调 | P0 |
| 10 | POST | `/api/v1/smart-badge/process/pending` | 处理待处理事件 | P0 |
| 11 | GET | `/api/v1/smart-badge/me` | 我的工牌状态 | P0 |
| 12 | POST | `/api/v1/smart-badge/me/recording/start` | 启动我的录音 | P0 |
| 13 | POST | `/api/v1/smart-badge/me/recording/stop` | 停止我的录音 | P0 |

### 数据表

| 表名 | 说明 |
|------|------|
| badge_devices | 设备主表 |
| badge_device_lifecycle_logs | 设备生命周期日志 |
| badge_tickets | 工单 |
| badge_manufacturers | 厂商 |
| badge_manufacturer_configs | 厂商配置 |
| badge_vendor_pool_devices | 厂家设备池 |
| badge_vendor_pool_sync_batches | 同步批次 |
| badge_vendor_pool_sync_batch_items | 同步批次项 |
| badge_recording_control_logs | 录音控制日志 |
| badge_pending_events | 待处理事件 |
| badge_device_tokens | 设备令牌 |

### 外部依赖
- badge-middleware（设备查询、录音控制、厂商回调转发）

---

## 七、content 模块（74 端点）

### 7.1 选题管理 — content.py（13 端点）

> 路径前缀：`/api/v1/content`

| # | 方法 | 路径 | 说明 | 优先级 |
|---|------|------|------|--------|
| 1 | GET | `/api/v1/content/hot-topics` | 热点话题 | P0 |
| 2 | POST | `/api/v1/content/hot-topics/refresh` | 刷新热点 | P1 |
| 3 | GET | `/api/v1/content/topics` | 选题列表 | P0 |
| 4 | GET | `/api/v1/content/topics/{topic_id}` | 选题详情 | P0 |
| 5 | POST | `/api/v1/content/topics/generate` | AI 生成选题 | P0 |
| 6 | POST | `/api/v1/content/topics` | 创建选题 | P0 |
| 7 | PUT | `/api/v1/content/topics/{topic_id}` | 更新选题 | P0 |
| 8 | PUT | `/api/v1/content/topics/{topic_id}/select` | 选定选题 | P1 |
| 9 | DELETE | `/api/v1/content/topics/{topic_id}` | 删除选题 | P1 |
| 10 | POST | `/api/v1/content/idea-topics/start` | "我有想法"初始化 | P1 |
| 11 | POST | `/api/v1/content/idea-topics/parse-files` | 解析文件 | P1 |
| 12 | POST | `/api/v1/content/idea-topics/generate` | 生成选题 | P1 |
| 13 | POST | `/api/v1/content/idea-topics/save-topics` | 保存选题 | P1 |

### 7.2 内容管理 — content.py（11 端点）

| # | 方法 | 路径 | 说明 | 优先级 |
|---|------|------|------|--------|
| 1 | GET | `/api/v1/content/contents` | 内容列表 | P0 |
| 2 | GET | `/api/v1/content/contents/{content_id}` | 内容详情 | P0 |
| 3 | POST | `/api/v1/content/contents/generate` | AI 生成内容 | P0 |
| 4 | POST | `/api/v1/content/contents` | 创建内容 | P0 |
| 5 | PUT | `/api/v1/content/contents/{content_id}` | 更新内容 | P0 |
| 6 | DELETE | `/api/v1/content/contents/{content_id}` | 删除内容 | P1 |
| 7 | POST | `/api/v1/content/contents/{content_id}/generate-images` | 批量生成图片 | P1 |
| 8 | POST | `/api/v1/content/contents/{content_id}/generate-single-image` | 生成单张图片 | P1 |
| 9 | POST | `/api/v1/content/contents/{content_id}/save-composed-images` | 保存合成图片 | P1 |
| 10 | POST | `/api/v1/content/contents/{content_id}/publish` | 发布内容 | P0 |
| 11 | POST | `/api/v1/content/contents/{content_id}/unpublish` | 取消发布 | P1 |

### 7.3 会话洞察 — content.py（4 端点）

| # | 方法 | 路径 | 说明 | 优先级 |
|---|------|------|------|--------|
| 1 | GET | `/api/v1/content/conversation-insights/stats` | 洞察统计 | P1 |
| 2 | GET | `/api/v1/content/conversation-insights/frequent-questions` | 高频问题 | P1 |
| 3 | POST | `/api/v1/content/conversation-insights/mine-topics` | 选题挖掘 | P1 |
| 4 | POST | `/api/v1/content/conversation-insights/save-topics` | 保存挖掘选题 | P1 |

### 7.4 发布任务 — content.py（9 端点）

| # | 方法 | 路径 | 说明 | 优先级 |
|---|------|------|------|--------|
| 1 | GET | `/api/v1/content/publish-tasks/dashboard` | 发布工作台 | P0 |
| 2 | GET | `/api/v1/content/publish-tasks` | 任务列表 | P0 |
| 3 | GET | `/api/v1/content/publish-tasks/{task_id}` | 任务详情 | P0 |
| 4 | POST | `/api/v1/content/publish-tasks` | 创建任务 | P0 |
| 5 | POST | `/api/v1/content/publish-tasks/batch` | 批量创建 | P1 |
| 6 | PUT | `/api/v1/content/publish-tasks/{task_id}/status` | 更新状态 | P0 |
| 7 | PUT | `/api/v1/content/publish-tasks/{task_id}/cancel` | 取消任务 | P1 |
| 8 | PUT | `/api/v1/content/publish-tasks/{task_id}/retry` | 重试任务 | P1 |
| 9 | DELETE | `/api/v1/content/publish-tasks/{task_id}` | 删除任务 | P1 |

### 7.5 GEO 优化 — content.py（4 端点）

| # | 方法 | 路径 | 说明 | 优先级 |
|---|------|------|------|--------|
| 1 | POST | `/api/v1/content/geo/analyze` | GEO 分析 | P1 |
| 2 | POST | `/api/v1/content/geo/analyze-by-id/{content_id}` | 按 ID 分析 | P1 |
| 3 | POST | `/api/v1/content/geo/optimize` | 深度优化 | P1 |
| 4 | GET | `/api/v1/content/geo/prompt-injection` | 策略注入 | P1 |

### 7.6 素材池 — content_seeds.py（11 端点）

> 路径前缀：`/api/v1/content-seeds`

| # | 方法 | 路径 | 说明 | 优先级 |
|---|------|------|------|--------|
| 1 | GET | `/api/v1/content-seeds` | 素材列表 | P0 |
| 2 | GET | `/api/v1/content-seeds/stats` | 素材统计 | P0 |
| 3 | GET | `/api/v1/content-seeds/clusters` | 关注聚类 | P1 |
| 4 | GET | `/api/v1/content-seeds/my-inspirations` | 我的灵感 | P1 |
| 5 | POST | `/api/v1/content-seeds/{seed_id}/generate-draft` | 生成草稿 | P0 |
| 6 | POST | `/api/v1/content-seeds/{seed_id}/dismiss` | 驳回素材 | P1 |
| 7 | GET | `/api/v1/content-seeds/{seed_id}` | 素材详情 | P0 |
| 8 | PATCH | `/api/v1/content-seeds/{seed_id}/status` | 更新状态 | P1 |
| 9 | GET | `/api/v1/content-seeds/honor-list` | 荣誉榜 | P1 |
| 10 | GET | `/api/v1/content-seeds/my-stats` | 我的统计 | P1 |
| 11 | GET | `/api/v1/content-seeds/my-adopted` | 我的采纳 | P1 |

### 7.7 通用提示词模板 — prompt_templates.py（13 端点）

> 路径前缀：`/api/v1/prompt-templates`

| # | 方法 | 路径 | 说明 | 优先级 |
|---|------|------|------|--------|
| 1 | GET | `/api/v1/prompt-templates` | 模板列表 | P0 |
| 2 | POST | `/api/v1/prompt-templates` | 创建模板 | P0 |
| 3 | GET | `/api/v1/prompt-templates/{template_id}` | 模板详情 | P0 |
| 4 | PUT | `/api/v1/prompt-templates/{template_id}` | 更新模板 | P0 |
| 5 | DELETE | `/api/v1/prompt-templates/{template_id}` | 删除模板 | P1 |
| 6 | POST | `/api/v1/prompt-templates/preview` | 预览渲染 | P1 |
| 7 | POST | `/api/v1/prompt-templates/{template_id}/clone` | 克隆模板 | P1 |
| 8 | POST | `/api/v1/prompt-templates/{template_id}/versions` | 创建版本 | P0 |
| 9 | GET | `/api/v1/prompt-templates/{template_id}/versions` | 版本列表 | P0 |
| 10 | POST | `/api/v1/prompt-templates/{template_id}/publish` | 发布版本 | P0 |
| 11 | POST | `/api/v1/prompt-templates/{template_id}/rollback/{version}` | 回滚版本 | P1 |
| 12 | POST | `/api/v1/prompt-templates/{template_id}/test` | 测试模板 | P1 |
| 13 | GET | `/api/v1/prompt-templates/{template_id}/stats` | 使用统计 | P2 |

### 7.8 内容提示词模板 — content_prompt_templates.py（9 端点）

> 路径前缀：`/api/v1/content-prompt-templates`

| # | 方法 | 路径 | 说明 | 优先级 |
|---|------|------|------|--------|
| 1 | GET | `/api/v1/content-prompt-templates` | 模板列表 | P0 |
| 2 | POST | `/api/v1/content-prompt-templates` | 创建模板 | P0 |
| 3 | GET | `/api/v1/content-prompt-templates/{template_id}` | 模板详情 | P0 |
| 4 | PUT | `/api/v1/content-prompt-templates/{template_id}` | 更新模板 | P0 |
| 5 | DELETE | `/api/v1/content-prompt-templates/{template_id}` | 删除模板 | P1 |
| 6 | POST | `/api/v1/content-prompt-templates/{template_id}/clone` | 克隆模板 | P1 |
| 7 | POST | `/api/v1/content-prompt-templates/{template_id}/test` | 测试模板 | P1 |
| 8 | GET | `/api/v1/content-prompt-templates/{template_id}/stats` | 使用统计 | P2 |
| 9 | POST | `/api/v1/content-prompt-templates/initialize-defaults` | 初始化默认模板 | P1 |

### 数据表

| 表名 | 说明 |
|------|------|
| content_topics | 选题 |
| content_items | 内容主表 |
| content_publish_tasks | 发布任务 |
| content_seeds | 素材池（录音衍生） |
| concern_clusters | 关注聚类 |
| prompt_templates | 通用提示词模板 |
| prompt_template_versions | 模板版本 |
| content_prompt_templates | 内容提示词模板 |

### 外部依赖
- llm-gateway（选题生成、内容生成、GEO 优化、图片生成）
- 阿里云 OSS（图片存储）

---

## 八、customer 模块（35 端点）

### 8.1 客户管理 — customers.py（17 端点）

> 路径前缀：`/api/v1/customers`

| # | 方法 | 路径 | 说明 | 优先级 |
|---|------|------|------|--------|
| 1 | GET | `/api/v1/customers` | 客户列表 | P0 |
| 2 | GET | `/api/v1/customers/stats/overview` | 客户统计 | P0 |
| 3 | GET | `/api/v1/customers/{customer_id}` | 客户详情 | P0 |
| 4 | GET | `/api/v1/customers/{customer_id}/momentum-history` | 热度历史 | P1 |
| 5 | POST | `/api/v1/customers` | 创建客户 | P0 |
| 6 | PUT | `/api/v1/customers/{customer_id}` | 更新客户 | P0 |
| 7 | PUT | `/api/v1/customers/{customer_id}/converted` | 标记成交 | P0 |
| 8 | POST | `/api/v1/customers/{customer_id}/identities` | 添加渠道身份 | P1 |
| 9 | GET | `/api/v1/customers/{customer_id}/interactions` | 互动记录列表 | P0 |
| 10 | POST | `/api/v1/customers/{customer_id}/interactions` | 添加互动记录 | P0 |
| 11 | GET | `/api/v1/customers/{customer_id}/follow-ups` | 跟进记录列表 | P0 |
| 12 | POST | `/api/v1/customers/{customer_id}/follow-ups` | 创建跟进记录 | P0 |
| 13 | GET | `/api/v1/customers/{customer_id}/membership` | 会员信息 | P1 |
| 14 | GET | `/api/v1/customers/duplicates` | 重复检测 | P1 |
| 15 | POST | `/api/v1/customers/merge` | 客户合并 | P1 |
| 16 | GET | `/api/v1/customers/{customer_id}/consultation-records` | 咨询记录 | P1 |
| 17 | GET | `/api/v1/customers/{customer_id}/emr-records` | 病案记录 | P1 |

### 8.2 标签管理 — customer_groups.py（6 端点）

> 路径前缀：`/api/v1/customer-tags`

| # | 方法 | 路径 | 说明 | 优先级 |
|---|------|------|------|--------|
| 1 | GET | `/api/v1/customer-tags` | 标签列表 | P0 |
| 2 | POST | `/api/v1/customer-tags` | 创建标签 | P0 |
| 3 | PUT | `/api/v1/customer-tags/{tag_id}` | 更新标签 | P0 |
| 4 | DELETE | `/api/v1/customer-tags/{tag_id}` | 删除标签 | P1 |
| 5 | GET | `/api/v1/customer-tags/{tag_id}/customers` | 标签下客户 | P0 |
| 6 | POST | `/api/v1/customer-tags/batch` | 批量打标 | P0 |

### 8.3 分组管理 — customer_groups.py（12 端点）

> 路径前缀：`/api/v1/customer-groups`

| # | 方法 | 路径 | 说明 | 优先级 |
|---|------|------|------|--------|
| 1 | GET | `/api/v1/customer-groups` | 分组列表 | P0 |
| 2 | POST | `/api/v1/customer-groups` | 创建分组 | P0 |
| 3 | PUT | `/api/v1/customer-groups/{group_id}` | 更新分组 | P0 |
| 4 | DELETE | `/api/v1/customer-groups/{group_id}` | 删除分组 | P1 |
| 5 | GET | `/api/v1/customer-groups/{group_id}/customers` | 分组客户列表 | P0 |
| 6 | POST | `/api/v1/customer-groups/{group_id}/calculate` | 智能计算 | P0 |
| 7 | POST | `/api/v1/customer-groups/{group_id}/members` | 添加成员 | P0 |
| 8 | DELETE | `/api/v1/customer-groups/{group_id}/members` | 移除成员 | P1 |
| 9 | POST | `/api/v1/customer-groups/rules/preview` | 规则预览 | P1 |
| 10 | POST | `/api/v1/customer-groups/rules/validate` | 规则验证 | P1 |
| 11 | GET | `/api/v1/customer-groups/rules/fields` | 可用字段 | P1 |
| 12 | GET | `/api/v1/customer-groups/rules/operators` | 可用操作符 | P1 |

### 数据表

| 表名 | 说明 |
|------|------|
| customers | 客户主表 |
| customer_identities | 渠道身份 |
| customer_interactions | 互动记录 |
| customer_follow_ups | 跟进记录 |
| customer_tags | 标签 |
| customer_tag_assignments | 标签关联 |
| customer_groups | 分组 |
| customer_group_members | 分组成员 |
| customer_group_rules | 分组规则 |
| customer_memberships | 会员信息 |

---

## 九、support 模块（36 端点）

### 9.1 通知中心 — notifications.py（8 端点）

> 路径前缀：`/api/v1/notifications`

| # | 方法 | 路径 | 说明 | 优先级 |
|---|------|------|------|--------|
| 1 | POST | `/api/v1/notifications/device-tokens/register` | 注册设备令牌 | P0 |
| 2 | POST | `/api/v1/notifications/device-tokens/unregister` | 注销设备令牌 | P0 |
| 3 | GET | `/api/v1/notifications/device-tokens/me` | 我的设备令牌 | P1 |
| 4 | POST | `/api/v1/notifications/push-to-app` | 推送通知 | P0 |
| 5 | GET | `/api/v1/notifications` | 通知列表 | P0 |
| 6 | GET | `/api/v1/notifications/unread-count` | 未读数量 | P0 |
| 7 | PUT | `/api/v1/notifications/{notification_id}/read` | 标记已读 | P0 |
| 8 | PUT | `/api/v1/notifications/mark-all-read` | 全部已读 | P1 |

### 9.2 操作日志 — logs.py（3 端点）

| # | 方法 | 路径 | 说明 | 优先级 |
|---|------|------|------|--------|
| 1 | GET | `/api/v1/logs/operations` | 操作日志列表 | P0 |
| 2 | GET | `/api/v1/logs/operations/stats` | 操作统计 | P1 |
| 3 | GET | `/api/v1/logs/operations/{log_id}` | 日志详情 | P1 |

### 9.3 LLM 配置 — llm_config.py（6 端点）

> 路径前缀：`/api/v1/llm/models`

| # | 方法 | 路径 | 说明 | 优先级 |
|---|------|------|------|--------|
| 1 | GET | `/api/v1/llm/models` | 模型列表 | P0 |
| 2 | POST | `/api/v1/llm/models` | 创建模型配置 | P0 |
| 3 | GET | `/api/v1/llm/models/{config_id}` | 模型详情 | P0 |
| 4 | PUT | `/api/v1/llm/models/{config_id}` | 更新模型配置 | P0 |
| 5 | DELETE | `/api/v1/llm/models/{config_id}` | 删除模型配置 | P1 |
| 6 | POST | `/api/v1/llm/models/{config_id}/set-default` | 设为默认 | P0 |

### 9.4 LLM 调用记录 — llm_records.py（3 端点）

| # | 方法 | 路径 | 说明 | 优先级 |
|---|------|------|------|--------|
| 1 | GET | `/api/v1/llm/records` | 调用记录列表 | P0 |
| 2 | GET | `/api/v1/llm/records/stats` | 调用统计 | P1 |
| 3 | GET | `/api/v1/llm/records/{record_id}` | 记录详情 | P1 |

### 9.5 LLM 成本 — llm_cost.py（2 端点）

| # | 方法 | 路径 | 说明 | 优先级 |
|---|------|------|------|--------|
| 1 | GET | `/api/v1/llm/cost/tenant/{tenant_id}` | 租户成本 | P1 |
| 2 | GET | `/api/v1/llm/cost/summary` | 成本汇总 | P1 |

### 9.6 元数据 — metadata.py（2 端点）

| # | 方法 | 路径 | 说明 | 优先级 |
|---|------|------|------|--------|
| 1 | GET | `/api/v1/metadata/fields` | 可用字段列表 | P1 |
| 2 | POST | `/api/v1/metadata/validate-template` | 模板验证 | P1 |

### 9.7 数据浏览器 — data_browser.py（7 端点）

> 路径前缀：`/api/v1/data-browser`

| # | 方法 | 路径 | 说明 | 优先级 |
|---|------|------|------|--------|
| 1 | GET | `/api/v1/data-browser/tables` | 表列表 | P1 |
| 2 | GET | `/api/v1/data-browser/tables/{table_name}/structure` | 表结构 | P1 |
| 3 | GET | `/api/v1/data-browser/tables/{table_name}/data` | 表数据 | P1 |
| 4 | GET | `/api/v1/data-browser/tables/{table_name}/export` | 导出数据 | P2 |
| 5 | GET | `/api/v1/data-browser/statistics` | 数据库统计 | P1 |
| 6 | DELETE | `/api/v1/data-browser/tables/{table_name}/truncate` | 清空表 | P2 |
| 7 | POST | `/api/v1/data-browser/clear-import-data` | 清除导入数据 | P2 |

### 9.8 就诊管理 — visits.py（5 端点）

> 路径前缀：`/api/v1/visits`

| # | 方法 | 路径 | 说明 | 优先级 |
|---|------|------|------|--------|
| 1 | GET | `/api/v1/visits/health` | 健康检查 | P2 |
| 2 | GET | `/api/v1/visits` | 就诊列表 | P0 |
| 3 | GET | `/api/v1/visits/statistics` | 就诊统计 | P1 |
| 4 | GET | `/api/v1/visits/filters` | 筛选条件 | P1 |
| 5 | GET | `/api/v1/visits/{visit_id}` | 就诊详情 | P0 |

### 数据表

| 表名 | 说明 |
|------|------|
| notifications | 通知 |
| device_tokens | 推送设备令牌 |
| operation_logs | 操作日志 |
| llm_model_configs | LLM 模型配置 |
| llm_call_records | LLM 调用记录 |
| op_visits | 门诊就诊记录 |

### 外部依赖
- Expo Push API（移动端推送通知）
