# badge 拉取去重修复 · 发布说明（2026-11-21）

> 给发布 Agent 的操作说明。目标：把「工牌录音拉取去重修复」发布到生产。
> 有任何一步不确定，先停下问 lead 会话，不要猜测。

---

## 一、这次修了什么（背景一句话）

工牌录音拉取是「先上传 OSS、后查重」，导致同一批录音每 2 分钟被重复上传，桶里积累了约 2TB 重复孤儿对象。
本次修复：**worker 拉取加时间游标 + 入库前先查重；api 回调入库前先查重**。
详细背景见 `docs/OSS成本优化方案-讨论稿.md`（2.9 / 2.10 节）。

## 二、代码改动清单（两个仓库，当前**未提交**，发布前先 commit 或直接按工作区构建）

**lingce-worker（5 个文件）**
- `internal/gateway/badge.go` —— 拉取请求加 start_time/stop_time
- `internal/base/pull.go` —— 拉取循环加时间游标（5 分钟重叠）
- `internal/base/ingest.go` —— 入库前先按 order_no 查重
- `internal/repository/recordings.go` —— 新增 ExistsByOrderNo
- `internal/gateway/badge_test.go` —— 适配

**lingce-api（1 个文件）**
- `internal/badge/store.go` —— 回调入库前先按 event_id 查重

## 三、构建（linux amd64）

```bash
cd /Users/yiliiang/Documents/lingce-worker
GOOS=linux GOARCH=amd64 go build -o bin/lingce-worker ./cmd/lingce-worker

cd /Users/yiliiang/Documents/lingce-api
make build-linux   # 产物 bin/lingce-api
```

## 四、⚠️ 生产配置必须还原的两项（今天 11:14 应急止血改过的）

文件：`/etc/lingce/lingce-worker.toml`

| 配置项 | 现在（应急值） | 必须改回 |
| --- | --- | --- |
| `badge_middleware_url` | `http://127.0.0.1:1` | **`http://127.0.0.1:18082`** |
| `badge_pull_interval_minutes` | `10080` | **`2`** |

> API 配置 `/etc/lingce/lingce-api.toml` **没有被动过**，不用改。
> 中间件（badge-middleware）、nginx、厂商回调地址全部**不用动**。

## 五、部署步骤（生产，root）

```bash
# 1) 备份旧二进制
cp -a /opt/lingce-worker/lingce-worker /opt/lingce-worker/lingce-worker.bak-20261121
cp -a /opt/lingce-api/lingce-api       /opt/lingce-api/lingce-api.bak-20261121

# 2) 上传（在本机执行）
scp bin/lingce-worker root@8.140.246.26:/opt/lingce-worker/lingce-worker
scp bin/lingce-api     root@8.140.246.26:/opt/lingce-api/lingce-api

# 3) 属主与执行位（生产上执行；worker 旧二进制属主是 root，统一改为 lingce）
chown lingce:lingce /opt/lingce-worker/lingce-worker /opt/lingce-api/lingce-api
chmod 755 /opt/lingce-worker/lingce-worker /opt/lingce-api/lingce-api

# 4) 还原第四节的两项配置，然后重启
systemctl restart lingce-worker
systemctl restart lingce-api
```

## 六、发布后验证清单

1. `journalctl -u lingce-worker -f` 出现 `runtime started ... "badge_pull_interval":"2m0s"`；
2. 每 2 分钟一条 `badge pull summary pulled=X inserted=Y skipped=Z`；
3. 老录音全部 `skipped` 且 **OSS 不再新增 `UnixNano` 命名的对象**（观察 30 分钟）；
4. 新录音 `inserted>0`，正常入库转写；
5. API 日志出现 `audio callback already ingested, skip re-upload`（重复回调被短路）；
6. OSS 侧会由 lead 会话在发布后跑 `GetBucketStat` 复核。

## 七、注意事项（务必遵守）

1. **不要重启本机（Mac）的 dev worker**——它写的是生产桶 `lince`，已停止，保持停止。
2. **存量约 2TB 重复对象本次不删**——清理是独立任务，需要 DB 交叉清单，另行安排。
3. 重启后第一轮会按厂商默认窗口回填最近 24 小时，全部被查重跳过——**属预期，不是故障**。
4. 厂商回调通道（audio.khgl.xyz）保持现状，不动。
5. 发布完成后回告 lead 会话，由 lead 做最终复核。
