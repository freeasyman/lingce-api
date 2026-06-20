# 机构端租户菜单权限控制现状审计报告

本文档是机构端租户菜单权限控制体系的现状审计报告。

它与以下文档的关系如下：

1. 上位目标文档：
   [INSTITUTION_MENU_PERMISSION_REARCHITECTURE.md](/Users/yiliiang/Documents/lingce-api/docs/INSTITUTION_MENU_PERMISSION_REARCHITECTURE.md)
2. 终态数据模型文档：
   [INSTITUTION_PERMISSION_UNIFICATION_PLAN.md](/Users/yiliiang/Documents/lingce-api/docs/INSTITUTION_PERMISSION_UNIFICATION_PLAN.md)
3. 实施顺序文档：
   [INSTITUTION_PERMISSION_UNIFICATION_EXECUTION_PLAN.md](/Users/yiliiang/Documents/lingce-api/docs/INSTITUTION_PERMISSION_UNIFICATION_EXECUTION_PLAN.md)

本文档不讨论“应该怎么设计终态”本身，而是严格回答以下问题：

1. 当前系统实际上是如何工作的
2. 套餐控制、角色授权、员工当前角色、前端菜单显示、路由守卫、后端权限判断分别依赖什么事实
3. 当前代码和数据库已经出现了哪些真实分叉
4. 历史决策为什么会把系统带到今天这个状态
5. 当前体系的错误、缺陷、风险分别是什么
6. 为什么不能继续靠局部补丁维持

## 1. 审计结论

本次审计的核心结论如下：

1. 当前机构端菜单权限不是单一系统，而是 `tenant_feature_*`、`institution_*`、`inst_*`、前端本地 manifest、兼容逻辑共同拼接出的混合系统
2. 套餐层、角色层、员工角色事实层、前端显示层、后端裁决层没有共享同一套冻结语义
3. 当前系统在多个关键点表现为 fail-open，而不是 fail-closed
4. 开发库已经不是“旧表待删除”，而是“新旧表并行且数据分叉”
5. 历史决策本质上是阶段性妥协，不是可长期维护的最终体系
6. 如果继续在现状上修补，只会继续扩大语义漂移和数据分叉

结论性的判断是：

当前体系不能再以“修一个接口”或“补一个同步逻辑”的方式收口，必须按全链路模型一次性重构。

## 2. 审计范围

本次审计覆盖以下层级：

1. 运营平台套餐 / 功能包菜单控制
2. 机构端角色菜单授权
3. 员工当前角色事实
4. 机构端前端菜单显示与路由守卫
5. 后端权限与菜单查询
6. 历史迁移决策
7. 开发库真实数据状态

本次审计重点不是某一张表，而是最终用户“为什么能看见某个菜单”这条链路上的全部事实源。

## 3. 当前系统的真实权限链路

按代码和数据库现状，当前机构端一个员工最终能否看到某菜单，实际上取决于以下几层混合事实：

1. 租户是否绑定 `tenant_feature_groups`
2. 若绑定，功能组里是否声明该菜单允许
3. 若未绑定，部分代码会直接把租户视为 unrestricted
4. 员工所在角色在 `inst_role_menus` 中是否被授权该菜单
5. 前端本地 manifest 是否收录该页面
6. 前端路径是否能被本地规则解析为菜单 code
7. 某些兼容菜单 code 是否被额外放行
8. 后端接口本身是否真的做了服务端权限拦截

这意味着：

当前“菜单权限”并不是数据库里某一张表决定的，而是跨层拼装出来的运行时结果。

## 4. 套餐 / 功能包层审计

## 4.1 设计职责本应是什么

套餐 / 功能包层正确的职责应当只有：

1. 定义某租户最多可以拥有的菜单上限
2. 定义某租户最多可以拥有的功能上限

它不应直接承担：

1. 员工菜单实际授权
2. 角色菜单补权
3. “没有配置时默认放开”的裁决

## 4.2 当前代码中的实际行为

在 [internal/rbac/store_institution.go](/Users/yiliiang/Documents/lingce-api/internal/rbac/store_institution.go:600) 的 `GetTenantAllowedMenuCodes` 中：

1. 系统先查 `tenant_feature_assignments`
2. 若查不到有效功能组，`groupID == nil`
3. 在 [internal/rbac/store_institution.go](/Users/yiliiang/Documents/lingce-api/internal/rbac/store_institution.go:619) 直接返回 `true, allowed, nil`

这表示：

1. 没有绑定功能组时，租户被视为 unrestricted
2. 这不是“无菜单”，而是“全放开”

在 [internal/sysconfig/store.go](/Users/yiliiang/Documents/lingce-api/internal/sysconfig/store.go:949) 的 `GetEffectiveFeaturePolicy` 中：

1. 代码同样先读取功能组绑定
2. 当 `groupID == nil` 时进入 fallback 分支
3. 该逻辑整体仍把“未绑定功能组”视为可放开的状态，而不是严格拒绝

这说明套餐层在语义上已经不是“上限白名单”，而是带有“缺省放开”色彩的裁决层。

## 4.3 套餐层与角色层的越界耦合

在 [internal/sysconfig/store.go](/Users/yiliiang/Documents/lingce-api/internal/sysconfig/store.go:844) 的 `AssignFeatureGroupToTenant` 中：

1. 绑定功能组后
2. 会调用 [internal/sysconfig/store.go](/Users/yiliiang/Documents/lingce-api/internal/sysconfig/store.go:865) 的 `syncInstitutionRoleMenusForTenantFeatureGroup`

这意味着：

1. 运营平台改套餐，不只是改租户菜单上限
2. 它还会隐式同步角色菜单授权
3. 套餐层直接污染角色授权层

这是当前体系最危险的结构性问题之一。

因为一旦套餐层改动会写入角色授权层，就不再存在清晰的责任边界：

1. 运营平台以为自己在控制套餐
2. 实际却在改机构端角色权限
3. 后续无法判断某个角色菜单到底来自人工授权还是套餐同步

## 4.4 套餐层错误与风险

已确认问题：

1. 未绑定功能组时存在 fail-open
2. 套餐绑定操作会越权改角色授权
3. 功能组层与角色层没有稳定边界

风险：

1. 租户未配置套餐时可能看到超出预期的菜单
2. 套餐调整后会遗留历史角色菜单脏数据
3. 运营平台操作会改变机构端授权事实，导致审计不可解释
4. 任何“回收菜单”的动作都可能因为旧角色菜单残留而失效

## 5. 角色定义与角色菜单授权层审计

## 5.1 当前职责分裂

按现状代码和开发库，角色定义层与角色菜单授权层没有统一在同一个 `institution_*` 体系中。

实际情况是：

1. 角色定义主要已经迁到 `institution_roles`
2. 角色菜单授权仍以 `inst_role_menus` 为主
3. 菜单字典仍以 `inst_menus` 为主
4. `institution_menus`、`institution_role_menus` 终态表尚未真正落地

## 5.2 关键代码证据

在 [internal/rbac/store_institution.go](/Users/yiliiang/Documents/lingce-api/internal/rbac/store_institution.go:1010) 有明确注释：

`Use inst_role_menus as the canonical institution role authorization store.`

随后：

1. [internal/rbac/store_institution.go](/Users/yiliiang/Documents/lingce-api/internal/rbac/store_institution.go:1011) 删除旧授权时删的是 `inst_role_menus`
2. [internal/rbac/store_institution.go](/Users/yiliiang/Documents/lingce-api/internal/rbac/store_institution.go:1018) 新增授权时写的是 `inst_role_menus`
3. [internal/rbac/store_institution.go](/Users/yiliiang/Documents/lingce-api/internal/rbac/store_institution.go:1093) 角色权限查询读的是 `inst_menus`
4. [internal/rbac/store_institution.go](/Users/yiliiang/Documents/lingce-api/internal/rbac/store_institution.go:1094) 关联的是 `inst_role_menus`

这说明：

1. 运行时主授权面仍然是旧 `inst_*`
2. `institution_roles` 并没有带来真正统一
3. 当前所谓“新体系”只覆盖了角色定义的一部分

## 5.3 角色层的结构性错误

角色层当前至少有四类结构性错误：

1. 角色定义与授权表不在同一体系
2. 菜单字典与角色菜单绑定不在同一体系
3. `id` 与 `code` 双语义混用
4. 某些链路认为 `institution_roles` 是真源，某些链路仍要回落到旧表

这会导致：

1. “角色保存成功”不等于“角色权限已进入同一真源”
2. 重构任何一侧都必须同时兼容另一套旧表
3. 开发人员无法仅通过表名前缀判断其角色

## 5.4 角色层风险

1. 新表定义更新后，旧授权表不一定同步
2. 菜单字典变更依赖 `inst_menus`，无法直接对齐终态设计
3. 删除旧表前没有任何单点能证明新体系已真正接管
4. 继续叠加兼容逻辑只会增加维护成本

## 6. 员工当前角色事实层审计

## 6.1 当前并不存在单一真源

开发库确认以下表同时存在：

1. `institution_employee_roles`
2. `inst_employee_roles`

并且两者数据已经分叉，不是简单镜像。

## 6.2 开发库已确认的数据证据

截至 2026-06-04，开发库 `lingce_dev` 审计结果如下：

1. `institution_employee_roles` 存在，行数 42
2. `inst_employee_roles` 存在，行数 73
3. 可映射后的完全对齐记录只有 24 条
4. `inst_employee_roles` 独有 49 条
5. `institution_employee_roles` 独有 18 条
6. 存在无法映射到角色定义层的角色编码：`lingce_sales`
7. `inst_employee_roles` 中存在一员工多角色

已确认的一员工多角色样例：

1. tenant 1, employee 44: `doctor,employee`
2. tenant 1, employee 77: `employee,operating_manager`
3. tenant 1, employee 78: `employee,operating_manager`
4. tenant 1, employee 81: `doctor,employee`
5. tenant 3, employee 9: `admin,marketing_manager`

这说明员工当前角色事实层已经具备以下问题：

1. 真源分叉
2. 语义分叉
3. 数据脏化

## 6.3 历史决策证据

在 [ROLE_SOURCE_UNIFICATION_DEV_PLAN.md](/Users/yiliiang/Documents/lingce-api/docs/ROLE_SOURCE_UNIFICATION_DEV_PLAN.md:1) 中，历史决策明确写成：

1. 员工当前业务角色真源定为 `inst_employee_roles`
2. `institution_roles` 保留为角色定义层
3. `inst_role_menus` 保留为菜单绑定层

这说明历史上已经做过一次选择：

1. 为了尽快统一 worker、录音、badge、sandbox 等业务链路
2. 优先把“员工当前角色事实”收口到 `inst_employee_roles`
3. 而不是一次性把整个权限体系彻底统一到 `institution_*`

这个历史决策在当时有现实合理性，但它的性质是阶段性妥协，而不是最终架构。

因为它解决的是：

1. 主业务角色读取一致性

它没有解决的是：

1. 套餐层与角色层的边界
2. 菜单字典统一
3. 角色菜单授权统一
4. 前端权限语义外溢
5. 旧表命名体系长期混杂

## 6.4 员工角色事实层风险

1. 不同服务可能从不同表读取员工角色
2. 员工改角色后，不同链路观察到的结果可能不同
3. 多角色脏数据会让“当前角色”概念失真
4. 任何录音分析、worker 分流、badge、sandbox 等按角色判断的逻辑都有潜在不一致风险

## 7. 前端菜单显示与路由守卫层审计

## 7.1 前端当前不是被动消费权限，而是在自行定义权限语义

理想状态下，前端应：

1. 消费后端返回的有效菜单事实
2. 负责渲染与体验性阻断
3. 不自行发明权限规则

但当前 institution 前端仍在做大量本地推导。

涉及的基础文件包括：

1. [apps/institution/src/menu-manifest.ts](/Users/yiliiang/Documents/lingce-web/apps/institution/src/menu-manifest.ts:1)
2. [apps/institution/src/navigation.tsx](/Users/yiliiang/Documents/lingce-web/apps/institution/src/navigation.tsx:1)

这意味着菜单编码、页面归属、路由到菜单的映射并不完全来自后端，而大量依赖前端本地 manifest。

## 7.2 根路由存在 fail-open

在 [apps/institution/src/routes/__root.tsx](/Users/yiliiang/Documents/lingce-web/apps/institution/src/routes/__root.tsx:103)：

1. 前端先算 `tenantAllowed`
2. 再取 `roleAllowed`
3. 若 `roleAllowed.size > 0`，则求交集
4. 若 `roleAllowed.size === 0`，则直接把 `tenantAllowed` 当作 `effectiveAllowed`

对应逻辑见：

1. [apps/institution/src/routes/__root.tsx](/Users/yiliiang/Documents/lingce-web/apps/institution/src/routes/__root.tsx:106)
2. [apps/institution/src/routes/__root.tsx](/Users/yiliiang/Documents/lingce-web/apps/institution/src/routes/__root.tsx:107)
3. [apps/institution/src/routes/__root.tsx](/Users/yiliiang/Documents/lingce-web/apps/institution/src/routes/__root.tsx:109)

这表示：

1. 角色没有显式菜单授权时
2. 前端并不会视为“无菜单”
3. 而是回落成“租户允许的菜单都能看”

这就是明确的 fail-open。

## 7.3 路由守卫同样存在 fail-open

在 [apps/institution/src/routes/__root.tsx](/Users/yiliiang/Documents/lingce-web/apps/institution/src/routes/__root.tsx:118) 的 `isRouteAllowed` 里：

1. 先算 `allowedCodes`
2. 再算 `tenantAllowedOnly`
3. 当 `allowedCodes.size === 0` 时
4. 在 [apps/institution/src/routes/__root.tsx](/Users/yiliiang/Documents/lingce-web/apps/institution/src/routes/__root.tsx:128) 到 [apps/institution/src/routes/__root.tsx](/Users/yiliiang/Documents/lingce-web/apps/institution/src/routes/__root.tsx:130) 直接回退为 `tenantAllowedOnly.has(menuCode)`

结果是：

1. 侧边栏显示层 fail-open
2. 路由守卫层也 fail-open
3. 两层共同把“空角色授权”解释成“租户菜单全可见”

## 7.4 角色管理页也在放大套餐权限

在 [apps/institution/src/routes/roles/index.tsx](/Users/yiliiang/Documents/lingce-web/apps/institution/src/routes/roles/index.tsx:89) 的 `allowedMenuCodeSet` 中：

1. 当前端读不到策略
2. 或策略被视为 `unrestricted`
3. 在 [apps/institution/src/routes/roles/index.tsx](/Users/yiliiang/Documents/lingce-web/apps/institution/src/routes/roles/index.tsx:95) 到 [apps/institution/src/routes/roles/index.tsx](/Users/yiliiang/Documents/lingce-web/apps/institution/src/routes/roles/index.tsx:96) 直接返回全部本地菜单 code

这表示角色页在编辑角色菜单时：

1. 并不是在严格编辑“租户允许范围内的角色菜单”
2. 而是先把“无策略”解释成“全部菜单可选”

这会进一步放大套餐层 fail-open 的影响。

## 7.5 前端层错误与风险

已确认问题：

1. 前端大量依赖本地 manifest 和 path 推导菜单 code
2. 菜单渲染层 fail-open
3. 路由守卫层 fail-open
4. 角色管理页在策略缺失时给出全量菜单
5. 兼容菜单 code 存在额外放行逻辑

风险：

1. 页面可见性与后端真实授权不一致
2. 新增菜单时前端可能因本地映射未更新而失真
3. 角色页可能产生不符合套餐边界的授权结果
4. 只隐藏菜单但不限制接口会形成假安全

## 8. 后端裁决层审计

## 8.1 后端当前不是统一最终裁决者

理想上，后端应统一输出：

1. tenant upper bound
2. role grants
3. effective menus
4. sensitive API enforcement

但当前后端主要暴露的是多个中间事实：

1. 套餐允许菜单
2. 角色菜单
3. 员工角色

而不是稳定冻结后的单一有效菜单语义。

## 8.2 已确认的问题

1. 套餐层无绑定时可 unrestricted
2. 角色菜单查询仍落在 `inst_role_menus`
3. 员工角色事实层并没有单点冻结
4. 套餐绑定操作会改角色菜单
5. 前端需要自己再推导一轮

这说明后端当前更多是在提供片段事实，而不是提供最终裁决结果。

## 8.3 后端层风险

1. 敏感接口可能只依赖前端菜单隐藏而没有严格服务端拦截
2. 无法对外证明机构端菜单权限具备统一、闭环、可审计的控制
3. 任何合规、审计、安全答复都缺乏坚实技术依据

## 9. 历史决策回溯与判断

## 9.1 历史路线不是彻底统一，而是阶段性收口

从历史文档与提交记录可以确认，过去的路线不是“所有权限表全部统一到 `institution_*`”，而是：

1. 角色定义尽量向 `institution_roles` 收口
2. 员工当前业务角色向 `inst_employee_roles` 收口
3. 角色菜单绑定继续保留在 `inst_role_menus`

这条路线的目标是：

1. 优先把业务角色判断统一
2. 降低对 worker、录音、badge、sandbox 的冲击
3. 用最小代价先让主链路跑通

## 9.2 对历史决策的评价

这个历史决策不是错误决策，但它不是最终合理终态。

更准确的判断是：

1. 它在当时是现实约束下的妥协产物
2. 它解决了局部链路一致性问题
3. 它把全链路统一问题延期了
4. 它也留下了长期命名混乱、语义混乱、表层混合的债务

因此，今天如果目标变成“一次性实现干净、可维护的体系”，就不能再把历史妥协路线当成终态设计。

## 10. 开发库现状总表

截至 2026-06-04，开发库已确认表状态如下：

存在：

1. `institution_employee_roles`
2. `institution_roles`
3. `institution_department_roles`
4. `inst_employee_roles`
5. `inst_roles`
6. `inst_menus`
7. `inst_role_menus`
8. `inst_department_roles`

不存在：

1. `institution_menus`
2. `institution_role_menus`

已确认数据量：

1. `institution_employee_roles`: 42
2. `inst_employee_roles`: 73
3. `institution_roles`: 135
4. `inst_menus`: 102
5. `inst_role_menus`: 1604
6. `institution_department_roles`: 18
7. `inst_department_roles`: 7

这个状态足以证明：

当前系统不是“新表接管完毕，只差删旧表”，而是“新旧体系并存，且权限事实跨层分布”。

## 11. 为什么不能继续增量修补

如果继续局部修补，通常会落入以下错误路线：

1. 套餐层补一个同步逻辑
2. 前端层补一个兼容判断
3. 角色页补一个兜底
4. 员工角色双写或双读再维持一段时间

这类修补的共同问题是：

1. 不会消除双真源
2. 不会消除 fail-open
3. 不会消除套餐越界改角色授权
4. 不会消除前端自定义权限语义
5. 不会消除 `inst_*` / `institution_*` 长期混用

因此，继续修补并不是“风险更小”，而是“把风险继续隐藏在兼容逻辑里”。

## 12. 本次重构必须解决的事项

结合本次审计，后续重构至少必须一次性解决以下问题：

1. 冻结套餐层只表达租户菜单上限
2. 彻底移除套餐层对角色菜单的隐式同步
3. 冻结角色定义、菜单字典、角色菜单授权、员工当前角色的唯一真源
4. 冻结空配置一律 fail-closed
5. 后端输出单一的有效菜单事实
6. 前端不再自行发明权限语义
7. 清理历史脏数据并完成旧表退出
8. 统一最终命名到 `institution_*`

这些事项的终态设计和实施顺序，分别见：

1. [INSTITUTION_MENU_PERMISSION_REARCHITECTURE.md](/Users/yiliiang/Documents/lingce-api/docs/INSTITUTION_MENU_PERMISSION_REARCHITECTURE.md)
2. [INSTITUTION_PERMISSION_UNIFICATION_PLAN.md](/Users/yiliiang/Documents/lingce-api/docs/INSTITUTION_PERMISSION_UNIFICATION_PLAN.md)
3. [INSTITUTION_PERMISSION_UNIFICATION_EXECUTION_PLAN.md](/Users/yiliiang/Documents/lingce-api/docs/INSTITUTION_PERMISSION_UNIFICATION_EXECUTION_PLAN.md)

## 13. 最终判断

当前机构端菜单权限控制问题，已经不是单点 bug，也不是单纯数据库命名不统一。

它本质上是：

1. 历史妥协路线停在半迁移状态
2. 多层权限事实长期共存
3. 缺少单一语义裁决层
4. 多处默认放开
5. 新旧表和前端本地规则共同参与最终权限结果

因此，本次工作必须按“租户菜单权限控制全链路重构”来做，而不能再按“旧表清理”或“某个接口修复”来理解。
