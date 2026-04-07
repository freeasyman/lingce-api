#!/bin/bash
# API文档服务启动脚本

echo "🚀 启动 API 文档服务..."
echo ""

# 检查是否安装了 swagger-ui-watcher
if ! command -v swagger-ui-watcher &> /dev/null; then
    echo "❌ 未安装 swagger-ui-watcher"
    echo "请运行: npm install -g swagger-ui-watcher"
    exit 1
fi

# 获取脚本所在目录
SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" && pwd )"
PROJECT_ROOT="$( cd "$SCRIPT_DIR/.." && pwd )"
OPENAPI_FILE="$PROJECT_ROOT/docs/openapi.yaml"

# 检查文档文件是否存在
if [ ! -f "$OPENAPI_FILE" ]; then
    echo "❌ 找不到 OpenAPI 文档: $OPENAPI_FILE"
    exit 1
fi

echo "📄 文档文件: $OPENAPI_FILE"
echo "🌐 服务地址: http://localhost:8000"
echo ""
echo "按 Ctrl+C 停止服务"
echo ""

# 启动文档服务
cd "$PROJECT_ROOT"
swagger-ui-watcher docs/openapi.yaml
