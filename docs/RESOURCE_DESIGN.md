# lingce-api 资源定义与重构方案

> 状态：草案，待审阅
> 日期：2026-04-14

## 一、背景

当前项目存在三个结构性问题：

1. **数据访问层无单一归属** — `employees` 表被 employee、organization、badge 三个模块各自写了独立的 SQL；`customers` 表被 customer、organization 两个模块各自操作。
2. **路由碎片化** — 230+ 端点中存在大量重复：同一 handler 注册多条路径、sysconfig/config 双重别名、badge v1/v2 并行、尾部斜杠冗余。
3. **鉴权散落** — RBAC 表和 CRUD 端点已建立，但没有任何 handler 真正检查权限。授权逻辑靠 `if claims.UserType != "admin"` 硬编码在各 handler 中。

本文档定义重构后的资源体系和实施方案。

---

## 二、设计原则

1. **一张表一个 Store** — 每张数据库表只有一个 Go 文件负责读写，其他模块通过调用该 Store 获取数据。
2. **资源即端点** — 每个一级资源对应一组标准化的 RESTful 端点，不允许同一实体出现在多个路径下。
3. **子资源表达关系** — 从属关系通过子资源路径表达（如 `/customers/{id}/interactions`），不单独成为一级资源。
4. **动作表达非 CRUD 操作** — 业务流程通过 `/actions/{verb}` 表达（如 `/badge-devices/{id}/actions/assign`）。
5. **声明式鉴权** — 路由注册时声明所需的用户类型和权限码，由统一中间件执行，handler 内不出现鉴权代码。
6. **作用域隔离** — 租户隔离通过中间件自动注入 scope，handler 通过 context 获取，不手动解析 tenant_id。
7. **提示词各业务独立** — 不同业务的提示词表结构差异大，保持独立表，作为各自资源的子资源管理。

---

## 三、一级资源清单（20 个 + 1 个特殊端点）

| # | 资源 | 路径前缀 | 对应主表 | 说明 |
|---|------|---------|---------|------|
| 1 | auth | `/api/v1/auth` | operations_admins, employees | 认证流程（登录、验证码、SMS、会话） |
| 2 | tenant | `/api/v1/tenants` | tenants | 租户管理 |
| 3 | department | `/api/v1/departments` | departments | 科室管理 |
| 4 | employee | `/api/v1/employees` | employees | 员工管理（doctor 通过 role 过滤） |
| 5 | customer | `/api/v1/customers` | customers | 客户管理（patient 通过 type 过滤） |
| 6 | recording | `/api/v1/recordings` | recordings | 录音管理 |
| 7 | recording-task | `/api/v1/recording-tasks` | recording_tasks | 录音任务 |
| 8 | badge-device | `/api/v1/badge-devices` | badge_devices | 工牌设备 |
| 9 | badge-ticket | `/api/v1/badge-tickets` | badge_tickets | 设备工单 |
| 10 | content-topic | `/api/v1/content-topics` | content_topics | 内容选题 |
| 11 | content-item | `/api/v1/content-items` | content_items | 内容条目 |
| 12 | content-seed | `/api/v1/content-seeds` | content_seeds | 内容素材 |
| 13 | role | `/api/v1/roles` | ops_roles, inst_roles | 角色（scope 参数区分 ops/institution） |
| 14 | menu | `/api/v1/menus` | ops_menus, inst_menus | 菜单（scope 参数区分） |
| 15 | notification | `/api/v1/notifications` | notifications | 通知 |
| 16 | operation-log | `/api/v1/operation-logs` | operation_logs | 操作日志 |
| 17 | subscription-plan | `/api/v1/subscription-plans` | tenant_subscription_plans | 订阅计划（全局实体） |
| 18 | feature-group | `/api/v1/feature-groups` | tenant_feature_groups | 功能组（全局实体） |
| 19 | llm | `/api/v1/llm` | llm_model_configs, llm_call_records | LLM 模型配置、调用记录、成本核算 |
| 20 | visit | `/api/v1/visits` | op_visits | 就诊记录 |
| - | healthz | `GET /healthz` | - | 健康检查（不属于任何资源） |

---

## 四、各资源端点详细设计

### 4.1 auth — 认证

不是实体 CRUD，是认证流程端点。

```
POST   /api/v1/auth/login                    管理员登录
POST   /api/v1/auth/login/institution        机构员工登录
POST   /api/v1/auth/login/mobile             移动端登录
GET    /api/v1/auth/me                       获取当前用户信息
POST   /api/v1/auth/change-password          修改密码
GET    /api/v1/auth/captcha                  获取验证码图片
POST   /api/v1/auth/sms/send                 发送短信验证码
POST   /api/v1/auth/sms/login                短信验证码登录
```

### 4.2 tenant — 租户

```
GET    /api/v1/tenants                       租户列表（管理员：全部；员工：仅自己所属）
POST   /api/v1/tenants                       创建租户
GET    /api/v1/tenants/{id}                  租户详情
PUT    /api/v1/tenants/{id}                  更新租户
DELETE /api/v1/tenants/{id}                  删除租户

# 子资源：订阅
GET    /api/v1/tenants/{id}/subscription          当前订阅状态
POST   /api/v1/tenants/{id}/subscription/actions/{action}
       action: renew | upgrade | pause | cancel | activate
GET    /api/v1/tenants/{id}/subscription/events   订阅事件日志
GET    /api/v1/tenants/{id}/validity-logs         有效期变更日志

# 子资源：功能配置
GET    /api/v1/tenants/{id}/features              生效的功能策略
POST   /api/v1/tenants/{id}/feature-group         分配功能组
GET    /api/v1/tenants/{id}/feature-overrides      功能覆盖列表
PUT    /api/v1/tenants/{id}/feature-overrides      设置功能覆盖

# 子资源：组织概况
GET    /api/v1/tenants/{id}/profile               机构档案
PUT    /api/v1/tenants/{id}/profile               更新机构档案
GET    /api/v1/tenants/{id}/statistics             机构统计数据
GET    /api/v1/tenants/{id}/medical-specialties    医学专科列表
```

### 4.3 department — 科室

```
GET    /api/v1/departments                   科室列表（支持 tenant_id 过滤）
POST   /api/v1/departments                   创建科室
GET    /api/v1/departments/{id}              科室详情
PUT    /api/v1/departments/{id}              更新科室
DELETE /api/v1/departments/{id}              删除科室

# 子资源与动作
GET    /api/v1/departments/{id}/performance  科室绩效
GET    /api/v1/departments/health            科室健康检查
POST   /api/v1/departments/actions/sync-from-visits  从就诊数据同步科室
```

### 4.4 employee — 员工

doctor 不再是独立资源，通过 `role=doctor` 过滤。

```
GET    /api/v1/employees                     员工列表
       ?role=doctor                          过滤医生
       ?department_id=5                      按科室过滤
       ?tenant_id=3                          按租户过滤
       ?is_active=true                       按状态过滤
POST   /api/v1/employees                     创建员工
GET    /api/v1/employees/{id}                员工详情
PUT    /api/v1/employees/{id}                更新员工
DELETE /api/v1/employees/{id}                删除员工

# 动作
POST   /api/v1/employees/{id}/actions/reset-password   重置密码

# 子资源
GET    /api/v1/employees/{id}/assistants     获取助理列表
PUT    /api/v1/employees/{id}/assistants     更新助理列表
GET    /api/v1/employees/{id}/performance    员工绩效（原 doctor performance）
GET    /api/v1/employees/performance/summary 绩效汇总

# 动作
POST   /api/v1/employees/actions/sync-from-visits  从就诊数据同步员工
```

### 4.5 customer — 客户

patient 不再是独立资源，通过 `type=patient` 过滤。

```
GET    /api/v1/customers                     客户列表
       ?type=patient                         过滤患者
       ?status=lead                          按状态过滤
       ?assigned_to=12                       按负责人过滤
       ?tenant_id=3                          按租户过滤
POST   /api/v1/customers                     创建客户
GET    /api/v1/customers/{id}                客户详情
PUT    /api/v1/customers/{id}                更新客户
DELETE /api/v1/customers/{id}                删除客户

# 动作
PUT    /api/v1/customers/{id}/actions/convert         标记转化
POST   /api/v1/customers/actions/merge                合并客户
POST   /api/v1/customers/actions/sync-from-visits     从就诊数据同步

# 子资源：身份
POST   /api/v1/customers/{id}/identities              添加身份渠道

# 子资源：互动记录
GET    /api/v1/customers/{id}/interactions             互动列表
POST   /api/v1/customers/{id}/interactions             创建互动

# 子资源：跟进
GET    /api/v1/customers/{id}/follow-ups               跟进列表
POST   /api/v1/customers/{id}/follow-ups               创建跟进

# 子资源：会员
GET    /api/v1/customers/{id}/membership               会员信息

# 子资源：其他
GET    /api/v1/customers/{id}/momentum-history         动量历史
GET    /api/v1/customers/{id}/consultation-records     咨询记录
GET    /api/v1/customers/{id}/emr-records              病历记录
GET    /api/v1/customers/{id}/360                      360 度视图

# 查重
GET    /api/v1/customers/duplicates                    查重检测

# 统计
GET    /api/v1/customers/stats/overview                客户统计概览

# 子资源：标签（customer 的子资源，不是一级资源）
GET    /api/v1/customers/tags                          标签列表
POST   /api/v1/customers/tags                          创建标签
PUT    /api/v1/customers/tags/{id}                     更新标签
DELETE /api/v1/customers/tags/{id}                     删除标签
POST   /api/v1/customers/tags/batch                    批量打标
GET    /api/v1/customers/tags/stats                    标签统计

# 子资源：分组（customer 的子资源，不是一级资源）
GET    /api/v1/customers/groups                        分组列表
POST   /api/v1/customers/groups                        创建分组
PUT    /api/v1/customers/groups/{id}                   更新分组
DELETE /api/v1/customers/groups/{id}                   删除分组
GET    /api/v1/customers/groups/{id}/members           分组成员
POST   /api/v1/customers/groups/{id}/members           添加成员
DELETE /api/v1/customers/groups/{id}/members           移除成员
POST   /api/v1/customers/groups/rules/preview          规则预览
POST   /api/v1/customers/groups/rules/validate         规则校验
GET    /api/v1/customers/groups/rules/fields           可用字段
GET    /api/v1/customers/groups/rules/operators         可用运算符
```

### 4.6 recording — 录音

提示词作为 recording 的子资源管理，保持独立表结构。

```
GET    /api/v1/recordings                    录音列表
       ?tenant_id=3&employee_id=5            按租户、员工过滤
       ?status=completed                     按状态过滤
       ?scene=consultation                   按场景过滤
       ?source=badge                         按来源过滤
       ?date_from=2026-01-01&date_to=2026-03-31
POST   /api/v1/recordings                    创建录音
GET    /api/v1/recordings/{id}               录音详情
PUT    /api/v1/recordings/{id}               更新录音
DELETE /api/v1/recordings/{id}               删除录音

# 动作
POST   /api/v1/recordings/{id}/actions/transcribe          触发转写
POST   /api/v1/recordings/{id}/actions/analyze             触发分析
POST   /api/v1/recordings/{id}/actions/clean               触发清洗
POST   /api/v1/recordings/{id}/actions/dispatch-follow-ups 派发跟进任务
POST   /api/v1/recordings/{id}/actions/confirm-action      确认跟进动作
POST   /api/v1/recordings/{id}/actions/generate-opening    生成开场白
POST   /api/v1/recordings/{id}/actions/generate-ops-plan   生成运营计划
POST   /api/v1/recordings/{id}/actions/mark-highlight      标记高光
POST   /api/v1/recordings/{id}/actions/reanalyze           重新分析
POST   /api/v1/recordings/{id}/actions/confirm-follow-ups  确认跟进任务
POST   /api/v1/recordings/actions/batch-transcribe         批量转写
POST   /api/v1/recordings/actions/batch-delete             批量删除
POST   /api/v1/recordings/actions/upload                   上传录音

# 子资源
GET    /api/v1/recordings/{id}/analysis                    分析结果
POST   /api/v1/recordings/{id}/analysis/feedback           分析反馈
GET    /api/v1/recordings/{id}/play-url                    播放地址
GET    /api/v1/recordings/{id}/learning-recommendation     学习推荐
GET    /api/v1/recordings/{id}/ops-plan-jobs/{job_id}      运营计划任务状态
GET    /api/v1/recordings/{id}/tasks                       关联的任务列表
GET    /api/v1/recordings/{id}/route                       路径分析
GET    /api/v1/recordings/{id}/segue                       SEGUE 评分

# 统计
GET    /api/v1/recordings/stats/overview                   总览
GET    /api/v1/recordings/stats/by-scene                   按场景
GET    /api/v1/recordings/stats/by-source                  按来源
GET    /api/v1/recordings/stats/by-tenant                  按租户
GET    /api/v1/recordings/stats/duration-distribution      时长分布
GET    /api/v1/recordings/stats/daily                      每日统计

# 医疗录音高级分析
GET    /api/v1/recordings/quality-control                  质控看板
GET    /api/v1/recordings/doctor-ability                   医生能力排名
GET    /api/v1/recordings/communication-analysis           沟通分析
GET    /api/v1/recordings/weekly-meeting                   周会材料
GET    /api/v1/recordings/weekly-summary                   周报
GET    /api/v1/recordings/team-trends                      团队趋势
GET    /api/v1/recordings/best-practices                   最佳实践列表
POST   /api/v1/recordings/{id}/best-practice               添加最佳实践
DELETE /api/v1/recordings/{id}/best-practice               移除最佳实践
GET    /api/v1/recordings/followup-generation-mode         跟进生成模式
POST   /api/v1/recordings/followup-generation-mode         更新跟进生成模式

# 子资源：录音提示词（独立表 recording_prompts，不与其他业务合并）
GET    /api/v1/recordings/prompts                          提示词列表
POST   /api/v1/recordings/prompts                          创建提示词
GET    /api/v1/recordings/prompts/codes/{code}             按 code 获取
PUT    /api/v1/recordings/prompts/codes/{code}             更新提示词
DELETE /api/v1/recordings/prompts/codes/{code}             删除提示词
```

### 4.7 recording-task — 录音任务

```
GET    /api/v1/recording-tasks               任务列表
       ?tenant_id=3&assigned_to=5            过滤条件
       ?status=pending                       按状态过滤
GET    /api/v1/recording-tasks/{id}          任务详情
POST   /api/v1/recording-tasks/{id}/actions/complete   完成任务
POST   /api/v1/recording-tasks/{id}/actions/cancel     取消任务

# 看板
GET    /api/v1/recording-tasks/employees     任务相关员工列表
GET    /api/v1/recording-tasks/dashboard     任务看板
```

### 4.8 badge-device — 工牌设备

```
GET    /api/v1/badge-devices                 设备列表
       ?tenant_id=3&status=active            过滤条件
       ?manufacturer_code=xxx                按厂商过滤
GET    /api/v1/badge-devices/{id}            设备详情
PATCH  /api/v1/badge-devices/{id}            更新设备信息

# 生命周期动作
POST   /api/v1/badge-devices/actions/import              导入设备
POST   /api/v1/badge-devices/actions/batch-accept         批量验收
POST   /api/v1/badge-devices/actions/batch-assign         批量分配
POST   /api/v1/badge-devices/actions/batch-reclaim        批量回收
POST   /api/v1/badge-devices/{id}/actions/assign-tenant   分配给租户
POST   /api/v1/badge-devices/{id}/actions/assign-employee 分配给员工
POST   /api/v1/badge-devices/{id}/actions/reclaim-employee 从员工回收
POST   /api/v1/badge-devices/{id}/actions/reclaim-tenant  从租户回收
POST   /api/v1/badge-devices/{id}/actions/transfer        转移设备
POST   /api/v1/badge-devices/{id}/actions/health-check    健康检查
POST   /api/v1/badge-devices/actions/batch-health-check   批量健康检查
POST   /api/v1/badge-devices/actions/validate-acceptance  验收校验
POST   /api/v1/badge-devices/actions/vendor-check         厂商校验

# 巡检
POST   /api/v1/badge-devices/{id}/actions/inspect         巡检设备
POST   /api/v1/badge-devices/actions/batch-inspect        批量巡检
GET    /api/v1/badge-devices/inspection                   巡检设备列表
GET    /api/v1/badge-devices/{id}/live-status             实时状态
POST   /api/v1/badge-devices/{id}/actions/recording-test  录音测试

# 录音控制
GET    /api/v1/badge-devices/recording-control            录控设备列表
POST   /api/v1/badge-devices/{device_no}/actions/start-recording  开始录音
POST   /api/v1/badge-devices/{device_no}/actions/stop-recording   停止录音
GET    /api/v1/badge-devices/recording-control/logs       录控日志
GET    /api/v1/badge-devices/{device_no}/history          设备历史

# 个人工牌
GET    /api/v1/badge-devices/me                           我的工牌状态
POST   /api/v1/badge-devices/me/actions/start-recording   开始我的录音
POST   /api/v1/badge-devices/me/actions/stop-recording    停止我的录音

# 子资源：设备日志
GET    /api/v1/badge-devices/{id}/logs                    操作日志
GET    /api/v1/badge-devices/{id}/lifecycle               生命周期日志

# 子资源：厂商（独立表，但作为设备域的子资源）
GET    /api/v1/badge-devices/manufacturers                厂商列表
PUT    /api/v1/badge-devices/manufacturers/{code}/config  更新厂商配置
POST   /api/v1/badge-devices/manufacturers/{code}/actions/sync  同步厂商数据

# 子资源：厂商设备池
POST   /api/v1/badge-devices/vendor-pool/actions/sync     同步设备池
POST   /api/v1/badge-devices/vendor-pool/actions/sync-and-diff  同步并对比
GET    /api/v1/badge-devices/vendor-pool/diff             差异列表
GET    /api/v1/badge-devices/vendor-pool/sync-batches     同步批次列表
GET    /api/v1/badge-devices/vendor-pool/sync-batches/{id}/items  批次明细
POST   /api/v1/badge-devices/vendor-pool/sync-batches/{id}/actions/rollback  回滚草稿
POST   /api/v1/badge-devices/vendor-pool/actions/create-acceptance-drafts    创建验收草稿
POST   /api/v1/badge-devices/vendor-pool/actions/mark-pending-assignment     标记待分配
POST   /api/v1/badge-devices/vendor-pool/actions/create-exception-tickets    创建异常工单

# 回调（外部系统调用）
POST   /api/v1/badge-devices/callbacks/developer          开发者回调
POST   /api/v1/badge-devices/callbacks/audio              音频回调
POST   /api/v1/badge-devices/actions/process-pending      处理待处理事件

# 导出与看板
GET    /api/v1/badge-devices/export                       导出设备
GET    /api/v1/badge-devices/dashboard                    设备看板
GET    /api/v1/badge-devices/tenant-overview              租户设备概览
```

### 4.9 badge-ticket — 设备工单

```
GET    /api/v1/badge-tickets                 工单列表
POST   /api/v1/badge-tickets                 提交工单
POST   /api/v1/badge-tickets/by-device       按设备提交工单
GET    /api/v1/badge-tickets/my              我的工单
GET    /api/v1/badge-tickets/{id}            工单详情
POST   /api/v1/badge-tickets/{id}/actions/review   审核工单
POST   /api/v1/badge-tickets/{id}/actions/execute   执行工单
```

### 4.10 content-topic — 内容选题

```
GET    /api/v1/content-topics                选题列表
POST   /api/v1/content-topics                创建选题
GET    /api/v1/content-topics/{id}           选题详情
PUT    /api/v1/content-topics/{id}           更新选题
DELETE /api/v1/content-topics/{id}           删除选题

# 动作
POST   /api/v1/content-topics/actions/generate           AI 生成选题
POST   /api/v1/content-topics/{id}/actions/select         选用选题

# 子资源：对话洞察
GET    /api/v1/content-topics/insights/stats              洞察统计
GET    /api/v1/content-topics/insights/frequent-questions  高频问题
POST   /api/v1/content-topics/insights/actions/mine-topics 挖掘选题
```

### 4.11 content-item — 内容条目

提示词作为 content-item 的子资源管理，保持独立表结构。

```
GET    /api/v1/content-items                 内容列表
POST   /api/v1/content-items                 创建内容
GET    /api/v1/content-items/{id}            内容详情
PUT    /api/v1/content-items/{id}            更新内容
DELETE /api/v1/content-items/{id}            删除内容

# 动作
POST   /api/v1/content-items/actions/generate             AI 生成内容
POST   /api/v1/content-items/{id}/actions/publish          发布
POST   /api/v1/content-items/{id}/actions/unpublish        下架
POST   /api/v1/content-items/actions/generate-image        生成图片
POST   /api/v1/content-items/actions/batch-generate-images 批量生成图片

# 子资源：GEO 优化
POST   /api/v1/content-items/{id}/geo/analyze              GEO 分析
POST   /api/v1/content-items/{id}/geo/optimize             GEO 优化
GET    /api/v1/content-items/geo/prompt-injection           GEO 提示注入

# 子资源：发布任务
GET    /api/v1/content-items/{id}/publish-tasks            发布任务列表
POST   /api/v1/content-items/{id}/publish-tasks            创建发布任务

# 子资源：内容提示词（独立表 content_prompt_templates，不与其他业务合并）
GET    /api/v1/content-items/prompts                       提示词列表
POST   /api/v1/content-items/prompts                       创建提示词
GET    /api/v1/content-items/prompts/{id}                  提示词详情
PUT    /api/v1/content-items/prompts/{id}                  更新提示词
DELETE /api/v1/content-items/prompts/{id}                  删除提示词
```

### 4.12 content-seed — 内容素材

```
GET    /api/v1/content-seeds                 素材列表
GET    /api/v1/content-seeds/{id}            素材详情

# 动作
POST   /api/v1/content-seeds/{id}/actions/adopt            采纳
POST   /api/v1/content-seeds/{id}/actions/dismiss           忽略

# 统计与聚类
GET    /api/v1/content-seeds/stats                         素材统计
GET    /api/v1/content-seeds/clusters                      关注点聚类
GET    /api/v1/content-seeds/honor-list                    荣誉榜
```

### 4.13 role — 角色

通过 `scope` 参数区分运营平台角色和机构角色，共用端点模式。

```
GET    /api/v1/roles                         角色列表
       ?scope=ops                            运营平台角色
       ?scope=institution                    机构角色
POST   /api/v1/roles                         创建角色
GET    /api/v1/roles/{id}                    角色详情
PUT    /api/v1/roles/{id}                    更新角色
DELETE /api/v1/roles/{id}                    删除角色

# 子资源：权限
GET    /api/v1/roles/{id}/permissions        角色权限列表
POST   /api/v1/roles/{id}/permissions        分配权限
DELETE /api/v1/roles/{id}/permissions        移除权限

# 子资源：菜单
GET    /api/v1/roles/{id}/menus              角色菜单列表
PUT    /api/v1/roles/{id}/menus              分配菜单

# 子资源：管理员（仅 scope=ops）
GET    /api/v1/roles/admins                  管理员列表
POST   /api/v1/roles/admins                  创建管理员
GET    /api/v1/roles/admins/{id}             管理员详情
PUT    /api/v1/roles/admins/{id}             更新管理员
DELETE /api/v1/roles/admins/{id}             删除管理员
POST   /api/v1/roles/admins/{id}/actions/reset-password  重置密码
```

### 4.14 menu — 菜单

```
GET    /api/v1/menus                         菜单列表
       ?scope=ops                            运营平台菜单
       ?scope=institution                    机构菜单
GET    /api/v1/menus/tree                    菜单树
POST   /api/v1/menus                         创建菜单
GET    /api/v1/menus/{id}                    菜单详情
PUT    /api/v1/menus/{id}                    更新菜单
DELETE /api/v1/menus/{id}                    删除菜单
PUT    /api/v1/menus/sort                    菜单排序
```

### 4.15 notification — 通知

```
GET    /api/v1/notifications                 通知列表
GET    /api/v1/notifications/{id}            通知详情
POST   /api/v1/notifications/{id}/actions/read   标记已读
POST   /api/v1/notifications/actions/read-all    全部已读

# 子资源：推送令牌
POST   /api/v1/notifications/device-tokens        注册设备令牌
DELETE /api/v1/notifications/device-tokens/{id}    注销设备令牌
```

### 4.16 operation-log — 操作日志

```
GET    /api/v1/operation-logs                日志列表
GET    /api/v1/operation-logs/{id}           日志详情
GET    /api/v1/operation-logs/stats          日志统计

# 子资源：数据浏览器（管理员工具）
GET    /api/v1/operation-logs/data-browser/tables                    表列表
GET    /api/v1/operation-logs/data-browser/tables/{name}/structure   表结构
GET    /api/v1/operation-logs/data-browser/tables/{name}/data        表数据
GET    /api/v1/operation-logs/data-browser/tables/{name}/export      导出
DELETE /api/v1/operation-logs/data-browser/tables/{name}/truncate    清空
```

### 4.17 subscription-plan — 订阅计划

全局实体，不属于任何租户。

```
GET    /api/v1/subscription-plans            计划列表
POST   /api/v1/subscription-plans            创建计划
PUT    /api/v1/subscription-plans/{id}       更新计划
```

### 4.18 feature-group — 功能组

全局实体，不属于任何租户。

```
GET    /api/v1/feature-groups                功能组列表
POST   /api/v1/feature-groups                创建功能组
GET    /api/v1/feature-groups/{id}           功能组详情
PUT    /api/v1/feature-groups/{id}           更新功能组
DELETE /api/v1/feature-groups/{id}           删除功能组
GET    /api/v1/feature-groups/options        可用功能选项
```

### 4.19 llm — LLM 管理

模型配置、调用记录、成本核算。通用提示词（prompt_templates 表）也归入此处。

```
# 模型配置
GET    /api/v1/llm/models                    模型列表
POST   /api/v1/llm/models                    创建模型配置
GET    /api/v1/llm/models/{id}               模型详情
PUT    /api/v1/llm/models/{id}               更新模型配置
DELETE /api/v1/llm/models/{id}               删除模型配置
POST   /api/v1/llm/models/{id}/actions/set-default  设为默认

# 调用记录
GET    /api/v1/llm/records                   调用记录列表
GET    /api/v1/llm/records/{id}              调用记录详情
GET    /api/v1/llm/records/stats             调用统计

# 成本核算
GET    /api/v1/llm/costs/by-tenant/{tenant_id}   按租户成本
GET    /api/v1/llm/costs/summary                  成本汇总

# 通用提示词（prompt_templates 表，带版本管理）
GET    /api/v1/llm/prompts                   通用提示词列表
POST   /api/v1/llm/prompts                   创建通用提示词
GET    /api/v1/llm/prompts/{id}              提示词详情
PUT    /api/v1/llm/prompts/{id}              更新提示词
DELETE /api/v1/llm/prompts/{id}              删除提示词
GET    /api/v1/llm/prompts/{id}/versions     版本历史
POST   /api/v1/llm/prompts/{id}/actions/publish  发布版本
POST   /api/v1/llm/prompts/{id}/actions/rollback 回滚版本
```

### 4.20 visit — 就诊记录

```
GET    /api/v1/visits                        就诊列表
GET    /api/v1/visits/{id}                   就诊详情
GET    /api/v1/visits/statistics              就诊统计
GET    /api/v1/visits/filters                可用过滤条件
GET    /api/v1/visits/health                 数据健康检查
```

---

## 五、鉴权体系

### 5.1 当前问题

- RBAC 表已建立（operations_roles, operations_permissions, institution_roles 等），CRUD 端点已实现，但没有任何 handler 检查权限。
- 授权逻辑散落在各 handler 中，靠 `if claims.UserType != "admin"` 硬编码。
- 租户隔离在部分模块使用 `tenancy.ResolveScope()`，部分模块不使用。
- `UserTypeMobile` 已定义但无差异化处理。

### 5.2 目标架构：声明式路由鉴权

路由注册时声明鉴权要求，由统一中间件执行，handler 内不出现任何鉴权代码。

```go
type Route struct {
    Method           string
    Path             string
    Handler          http.HandlerFunc
    Auth             bool              // 是否需要登录
    AllowedUserTypes []string          // 允许的用户类型：admin, employee, mobile
    Permission       string            // 权限码（如 "employee:write"），空字符串表示不检查
    TenantScoped     bool              // 是否需要租户隔离
}
```

### 5.3 三种用户类型的访问矩阵

| 资源 | admin | employee | mobile |
|------|-------|----------|--------|
| auth | 部分（login, me, change-password） | 部分 | 部分 |
| tenant | 读写 | 只读（仅自己所属） | - |
| department | 读写 | 只读 | - |
| employee | 读写 | 只读 | 只读（仅自己） |
| customer | 读写 | 读写（租户内） | 只读（分配给自己的） |
| recording | 读写 | 读写（租户内） | 只读（自己的） |
| recording-task | 读写 | 读写（租户内） | 读写（分配给自己的） |
| badge-device | 读写 | 只读（租户内） | 只读（自己的设备） |
| badge-ticket | 读写 | 读写（租户内） | 提交（自己的） |
| content-topic | 读写 | 读写（租户内） | - |
| content-item | 读写 | 读写（租户内） | - |
| content-seed | 读写 | 只读（租户内） | - |
| role | 读写 | 只读（institution scope） | - |
| menu | 读写 | 只读（institution scope） | - |
| notification | - | 读写（自己的） | 读写（自己的） |
| operation-log | 读写 | - | - |
| subscription-plan | 读写 | - | - |
| feature-group | 读写 | - | - |
| llm | 读写 | - | - |
| visit | 读写 | 只读（租户内） | - |

### 5.4 鉴权中间件执行流程

```
请求进入
  → 1. JWT 验证（已有）
  → 2. 用户类型检查（AllowedUserTypes）
  → 3. 租户隔离（TenantScoped → ResolveScope 注入 context）
  → 4. RBAC 权限检查（Permission → 查询角色权限表）
  → 5. handler 执行（无鉴权代码）
```

---

## 六、重构实施方案

### 6.1 Phase 1 — 内部收敛（不改路由路径）

目标：消除 store 层代码重复，建立"一张表一个 store"的规范。

改动范围：
- `organization/store.go` — 删除查 employees 和 customers 表的函数
- `organization/service.go` — 改为依赖 `employee.Store` 和 `customer.Store`
- `badge/handler.go` 中的 `ListTenantEmployees` — 改为调用 `employee.Store`
- `employee/store.go` — 补充 organization 模块需要的查询方法（如 role 过滤）
- `customer/store.go` — 补充 organization 模块需要的查询方法（如 type 过滤）
- `cmd/lingce-api/main.go` — 修改依赖注入接线

不改动：路由路径、前端调用、数据库表结构。
预期效果：employees 表只有 1 个 store 操作，customers 表只有 1 个 store 操作。

### 6.2 Phase 2 — 新路由上线 + 旧路由代理

目标：按本文档的资源定义注册新路由，旧路由代理到新路由。

改动范围：
- 按 20 个资源重新组织 handler 注册
- 实现声明式路由鉴权中间件
- 旧路由改为内部代理，添加 `Sunset` 和 `Deprecation` 响应头
- 消除所有尾部斜杠重复、sysconfig/config 别名、badge v1 路由

前端配合：逐步将调用地址迁移到新路由。

### 6.3 Phase 3 — 清理

目标：删除所有旧路由和兼容代码。

前提：前端全部迁移完成。

改动范围：
- 删除旧路由注册代码
- 删除 handler 中的兼容逻辑
- 删除 badge v1 handler/service/store
- 删除 sysconfig 中的 /config/* 别名

---

## 七、端点数量对比

| 指标 | 当前 | 重构后 |
|------|------|--------|
| 一级资源 | 12 个模块，边界模糊 | 20 个资源，职责明确 |
| 路由总数 | ~230（含重复和别名） | ~190（零重复） |
| Store 操作 employees 表 | 3 处 | 1 处 |
| Store 操作 customers 表 | 2 处 | 1 处 |
| 鉴权方式 | handler 内 if 硬编码 | 声明式中间件 |
| RBAC | 已建未用 | 真正生效 |
| 尾部斜杠重复 | ~10 条 | 0 |
| sysconfig/config 别名 | ~20 条 | 0 |
| badge v1/v2 并行 | 两套完整代码 | 统一为 v1 |
