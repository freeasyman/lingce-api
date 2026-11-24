# 发布交接：修复「录音上传 413 Request Entity Too Large」

- **日期**：2026-11-24
- **优先级**：P0（超过约 2 分钟的录音**完全无法上传**，医生端直接报错）
- **影响面**：桌面录音端（Windows/macOS）+ 移动端 App，两者走同一个上传接口
- **改动类型**：**只改 nginx 配置**，不改代码、不用发版、无数据变更
- **预计耗时**：5 分钟（含验证）

---

## 0. 一句话结论

线上 API 域名的 nginx 请求体上限还是默认的 **1 MB**，而录音 MP3 约 **8 KB/秒**（64kbps），
所以**录音超过约 2 分钟就必然 413**。代码侧本来支持 200MB，只要把 nginx 放开即可。

---

## 1. 现象

桌面端上传较长录音失败，前端提示「上传失败」；抓包/复现得到的响应是 **nginx 返回的 HTML**：

```html
<html><head><title>413 Request Entity Too Large</title></head>
<body><center><h1>413 Request Entity Too Large</h1></center>
<hr><center>nginx/1.24.0</center></body></html>
```

注意：这是 **nginx 拦的**，请求根本没进到 Go 应用，所以应用日志里看不到这条请求。

## 2. 实测边界（生产 api.khgl.xyz，2026-11-24 12:31）

用同一套 multipart 请求（`duration` 故意给非法值，避免写数据），只改文件大小：

| 文件大小 | 结果 |
|---|---|
| 481,004 B（≈1 分钟） | 通过，返回业务错误 `{"code":"BAD_REQUEST","message":"duration 无效"}` |
| 1,440,908 B（≈3 分钟） | **413 nginx** |
| 2,401,100 B（≈5 分钟） | 413 nginx |
| 4,801,004 B（≈10 分钟） | 413 nginx |

→ 上限即 nginx 默认值 **1m = 1,048,576 字节**。
桌面端 MP3 为 **16kHz 单声道 64kbps ≈ 8 KB/秒**，**约 2.1 分钟**即触顶。

## 3. 根因

- 应用侧（`lingce-api`）早就支持大文件：
  - `mobileUploadMaxFormBytes` ≈ **200 MB** + 2MB multipart 余量；
  - 上传分支用 `http.ResponseController` 把读写超时放宽到 **15 分钟**；
- 但**线上 nginx 仍是默认 `client_max_body_size 1m`**；
- 这个问题在 `docs/手机录音multipart上传与旧analyze跳过-2026-11-10.md` 第 4 节就提示过
  「线上需要在服务器执行 `nginx -T | grep client_max_body_size` 确认」，**一直没落地**。

## 4. 修复（运维执行）

### 4.1 先确认现状

```bash
nginx -T | grep -n "client_max_body_size"
```

如果输出为空，或输出 `client_max_body_size 1m;`，即是本问题。

### 4.2 修改

在 **API 域名对应的 server 块**（若你们有 `/api/` 的 location，也可以放在该 location 内）加一行：

```nginx
server {
    server_name api.khgl.xyz;

    # 录音上传：应用侧支持 200MB，这里给 100m 足够（64kbps 约 3.5 小时录音）
    client_max_body_size 100m;

    # 大文件在慢网络下可能传很久，确保代理超时足够
    proxy_read_timeout 300s;
    proxy_send_timeout 300s;

    location /api/ {
        proxy_pass http://127.0.0.1:<API端口>;
        # 如已有其它配置保持不动
    }
}
```

> 说明：
> - `client_max_body_size` 放在 `server` 级最省事，对所有 `location` 生效；
> - 只放 `/api/` 的 location 内也可以，但**必须确认该 location 覆盖 `/api/v1/recordings/actions/upload`**；
> - `proxy_read_timeout/proxy_send_timeout` 不设也能跑通（应用侧已放宽 15 分钟），但慢网络下建议加上。

### 4.3 生效

```bash
nginx -t && nginx -s reload
```

## 5. 验证（必做）

### 5.1 配置已生效

```bash
nginx -T | grep -n "client_max_body_size"
# 期望看到：client_max_body_size 100m;
```

### 5.2 用 5MB 文件验证不再 413

在任意机器上（token 用一个有效员工 token，或让客户端直接测）：

```bash
# 造一个 5MB 的"假音频"（内容不重要，只看是否被 nginx 拦）
dd if=/dev/zero of=/tmp/big.mp3 bs=1m count=5
printf '\xff\xfb' | dd of=/tmp/big.mp3 bs=1 conv=notrunc 2>/dev/null   # 让嗅探能识别为 mp3

curl -s -o /tmp/r.txt -w "HTTP %{http_code}\n" \
  -X POST https://api.khgl.xyz/api/v1/recordings/actions/upload \
  -H "Authorization: Bearer <有效token>" \
  -F "file=@/tmp/big.mp3;type=audio/mpeg;filename=consultation.mp3" \
  -F "duration=999999" -F "source=desktop"
head -c 200 /tmp/r.txt
```

**判读**：

- ✅ 成功：返回 **HTTP 400** 且 body 是 `{"code":"BAD_REQUEST","message":"duration 无效"}`
  （说明 5MB 已经穿过 nginx，被应用正常处理）；
- ❌ 未生效：仍返回 **HTTP 413** 且 body 是 `<html>...nginx...</html>`
  → 改的不是这个域名/这个 location，回到 4.2 检查。

### 5.3 端到端验证

1. 用桌面端录 **3 分钟以上** → 结束 → 应提示「成功」；
2. 后台确认落库：
   ```sql
   SELECT id, duration, file_url, created_at
   FROM recordings
   WHERE source = 'desktop'
   ORDER BY id DESC LIMIT 3;
   ```
   新记录应出现，且 `duration` 与录制时长一致。

## 6. 回滚

删掉 `client_max_body_size 100m;`（及新增的超时行），然后：

```bash
nginx -t && nginx -s reload
```

回滚只影响大文件上传，不影响其它接口。

## 7. 影响面与风险

- **只放宽请求体上限**，不涉及路由、鉴权、数据；
- 风险：放宽后恶意超大请求会打到应用层，但应用层自身有 **200MB 硬上限**
  （`http.MaxBytesReader`），超出直接返回 413 JSON，不会把内存打爆；
- 桌面端与移动端 App **同一接口**，改一次两边都恢复。

## 8. 客户端侧已做的事（说明，非替代）

桌面端已发一版（2026-11-24 12:34 构建），把 413 的 nginx HTML 映射成中文提示：

> 录音太长，服务器拒绝接收（413），请联系管理员

**这只是提示优化，不解决 413 本身**——真正的修复是本文第 4 节的 nginx 改动。

## 9. 后续建议（不阻塞本次）

1. 把 nginx 配置**纳入仓库管理**（如 `deploy/nginx/`），避免"文档写了但服务器没改"再次发生；
2. 客户端可加"上传前预估体积"提示（例如 >1.5MB 时提醒"录音较长，请确认网络"），
   在服务器修复前作为兜底体验；
3. 若将来要支持超长录音（>40 分钟），再考虑分片上传或直传 OSS（预签名 URL）。
