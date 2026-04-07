# API 文档快速启动

## 方法1：使用启动脚本（推荐）

```bash
cd /Users/yiliiang/Documents/lingce-api
./scripts/start-docs.sh
```

然后在浏览器中打开 http://localhost:8000

## 方法2：直接命令

```bash
cd /Users/yiliiang/Documents/lingce-api
swagger-ui-watcher docs/openapi.yaml
```

## 方法3：使用 Makefile

```bash
cd /Users/yiliiang/Documents/lingce-api
make docs
```

## 常见问题

### 问题1：找不到 swagger-ui-watcher
**解决方案**：
```bash
npm install -g swagger-ui-watcher
```

### 问题2：端口被占用
**解决方案**：
```bash
# 查找占用8000端口的进程
lsof -i :8000

# 杀死进程
kill -9 <PID>
```

### 问题3：文档更新后不刷新
**解决方案**：
- 刷新浏览器页面（Cmd+R）
- 或重启文档服务

## 在线查看（无需安装）

访问 https://editor.swagger.io/ 并粘贴以下文件内容：
```
/Users/yiliiang/Documents/lingce-api/docs/openapi.yaml
```
