# 启动期兼容迁移调研

> 结论版：当前启动慢的根因不是单条 SQL，而是 API 启动时同步执行了一个巨大的兼容迁移批次。这个问题不能只按“启动必需 / 离线迁移”二分，还需要进一步拆成“版本化一次性迁移”和“启动期仅校验”。

## 1. 现状

启动入口在 `cmd/lingce-api/main.go:81-86`：

```go
slog.Info("applying compatibility migrations")
if err := store.ApplyCompatMigrations(ctx, pool); err != nil {
    ...
}
```

这意味着：

- API 进程每次重启都会同步跑迁移
- `/healthz` 也要等这一步完成后才可用
- 这不是后台任务，也不是一次性部署动作

## 2. 代码调研结果

`internal/store/compat_migration.go:13` 里的 `ApplyCompatMigrations` 不是一组轻量检查，而是一个超大的串行 SQL 批次。

我统计到：

- `stmts` 总数：465
- `ALTER`：254
- `CREATE`：154
- `UPDATE`：37
- `DO`：8
- `INSERT`：12

其中还有额外的后处理函数：

- `normalizeOperationsMenus`：`internal/store/compat_migration.go:1905-2034`
- `ensureOperationsNavigationMenus`：`internal/store/compat_migration.go:2037-2058`
- `ensureProductLibrarySchema`：`internal/store/compat_migration.go:2068-2153`
- `seedTenantProductCatalog`：`internal/store/compat_migration.go:2165-2205`
- `seedBuiltinAnalysisPipelines`：`internal/store/compat_migration.go:2346-2378`
- `seedDefaultAnalysisRoutes`：`internal/store/compat_migration.go:2381-2582`

这几个函数里还有多次 `Exec/Query/QueryRow`，所以真实数据库往返次数远高于“465 条 SQL”本身。

## 3. 库侧证据

我连到了配置里的目标库 `lingce_dev`，确认：

- 数据库可连接
- `pg_stat_statements` 没启用，无法直接拿历史慢 SQL 分布
- 当前库里最大的相关表并不算特别大，但足够触发表级操作成本

抽样结果：

| 表 | 总大小 | 估算行数 |
|---|---:|---:|
| `recordings` | 93 MB | 1711 |
| `recording_analysis_results` | 30 MB | 7440 |
| `notifications` | 2920 kB | 4012 |
| `analysis_step_runs` | 1376 kB | 1461 |
| `wecom_message_logs` | 776 kB | 1303 |
| `analysis_runs` | 704 kB | 741 |
| `institution_role_menus` | 496 kB | 1864 |

说明：

- 数据量不是超大，但启动时做大量 `ALTER/UPDATE/CREATE INDEX` 仍然会明显拖慢
- 一些 `UPDATE` 是整表级别的兼容修复，不是纯空操作
- `normalizeOperationsMenus` 会对 `operations_role_menus` 和 `operations_menus` 做事务内搬运/删除

## 4. 结构性问题

单纯分成“启动必需”和“离线迁移”还不够。

原因是：

- 新业务表结构变更不能每次启动都重新 `ALTER`
- `IF NOT EXISTS` 只是幂等，不代表便宜
- `ADD COLUMN`、`CREATE INDEX IF NOT EXISTS`、`UPDATE ... WHERE ...` 在部分库状态下仍会产生锁、扫描和目录检查成本
- 启动期同步做数据回填，会把“服务可用性”绑定到“历史数据整理进度”

所以真正的问题是：**把一次性 schema 演进和启动路径混在了一起**。

## 5. 推荐方案

### 5.1 目标原则

1. 启动期只做轻量校验，不做大迁移
2. 结构变更只执行一次，不能在每次重启重复跑
3. 数据回填和兼容整理单独拆分，明确触发方式
4. 不新增常驻任务，不新增独立迁移服务

### 5.2 落地方式

推荐保留在 API 仓库内完成，分三层：

#### A. 启动校验层

- API 启动时只检查当前 schema 版本
- 若版本低于要求，直接报错退出或提示先跑迁移
- 不再直接执行 `ApplyCompatMigrations`

#### B. 一次性迁移层

- 把表结构变更拆成独立 SQL 文件或版本化迁移
- 每个版本只跑一次
- 这些迁移仍然可以由 API 仓库里的命令入口执行

#### C. 数据回填层

- 像 `normalizeOperationsMenus`
- `seedDefaultAnalysisRoutes`
- `RealignTrialTenantValidity`

这类逻辑不放进正常启动链，改成显式执行的维护命令或部署步骤。

## 6. 对“会不会引入新组件”的回答

不会要求你引入新的常驻任务。

更稳的方案是：

- 仍然只维护一个 API 仓库
- 仍然只部署现有 API 二进制
- 只把迁移执行从“每次启动”挪到“部署时一次性执行”
- 如有需要，可在同一个二进制里加一个 `--migrate-only` 模式，避免新建独立工具链

也就是说，**不增加新的服务常驻进程**，但必须避免把大迁移继续挂在正常启动路径上。

## 7. 建议改造顺序

1. 给 `ApplyCompatMigrations` 加耗时分段日志，先确认最慢的几段
2. 把启动入口里的同步调用移出正常启动路径
3. 建一个版本检查机制，启动时只校验，不重跑
4. 把 `compat_migration.go` 拆成：
   - schema version / bootstrap
   - 一次性结构迁移
   - 数据回填
   - 启动校验
5. 逐步把已有 `scripts/sql/*.sql` 归并成版本化迁移资产

## 8. 当前结论

这次慢启动的直接原因是：

- `cmd/lingce-api/main.go:81-86` 在启动主流程里同步执行兼容迁移
- `internal/store/compat_migration.go` 内部有 465 条语句，且包含多段表级修复与回填

要真正解决问题，不能只做“逻辑分类”，而要把**一次性迁移**和**每次启动**彻底分离。
