# API 迁移体系落地状态

> 结论先说：当前已经把“启动时同步跑大迁移”改成了“启动只校验、迁移走显式模式”，但“每次开发新功能都自动带迁移、且完全不漏”的严密体系还没有完全闭环。

## 已完成

1. 新增了版本化 schema migration runner。
2. 新增了 `api_schema_migrations` 账本表。
3. 新增了 `--migrate-only` 模式。
4. 正常启动路径已经改成先校验 schema 版本，再放行 HTTP 服务。
5. 新增了迁移文件生成脚本和连续性检查。
6. 新增了 DB 变更与 migration 绑定检查。
7. 新增了 CI 校验和 PR 模板。
8. 新增了 embedded baseline migration。

对应实现：

- `internal/store/schema_migration.go`
- `internal/store/migrations/20260807_001_baseline.sql`
- `cmd/lingce-api/main.go`
- `scripts/new_migration.sh`
- `scripts/check_migrations.sh`
- `scripts/check_db_migration_link.sh`
- `.github/workflows/migration-check.yml`
- `.github/pull_request_template.md`

## 还没完成

1. 旧的 `internal/store/compat_migration.go` 仍然保留大量历史兼容 SQL。
2. `--migrate-only` 现在仍会执行兼容迁移，不只是纯版本化迁移。
3. 现有 `scripts/sql/*.sql` 还没有全部归并进 `internal/store/migrations/`。
4. CI 现在已经补上了“DB 敏感文件变更必须带迁移文件”的门禁，但还可以继续细化敏感路径。
5. 对数据回填、历史修复、一次性修正，仍然没有完全统一到单一迁移策略里。

## 这意味着什么

- 不是新建常驻任务。
- 也不是新建独立迁移服务。
- 仍然只在 API 仓库内完成。
- 但目前还没有达到“任何 schema 变化都被强制绑定到迁移文件”的最后一层约束。

## 当前推荐使用方式

1. 开发新增 schema 变化时，先加 `internal/store/migrations/*.sql`。
2. 再改 Go 代码。
3. 部署前先执行一次 `./bin/lingce-api --config ./configs/dev.toml --migrate-only`。
4. 正常启动只做版本校验，不再跑大兼容迁移。

## 要补齐成严密体系，还差的关键一步

需要继续补的是：

- 旧式 `scripts/sql/*.sql` 要么迁入版本化目录，要么明确标记为临时维护脚本。
- 历史兼容逻辑要逐步从 `compat_migration.go` 里退场。

## 结论

现在的状态已经能解决启动慢的问题，也满足“不引入常驻任务”的约束。
但如果目标是“每次开发新功能都自动产生迁移动作，且无遗漏”，还需要补最后一层 CI/流程硬约束。
