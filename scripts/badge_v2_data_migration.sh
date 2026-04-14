#!/usr/bin/env bash
# Badge V2 数据迁移脚本
# 用途：检查现有数据并进行状态映射

set -e

# 配置
BASE_URL="${BASE_URL:-http://127.0.0.1:18080}"
TOKEN="${TOKEN:-}"

if [ -z "$TOKEN" ]; then
  echo "错误：请设置 TOKEN 环境变量"
  echo "用法：TOKEN=your_jwt_token bash $0"
  exit 1
fi

echo "=========================================="
echo "工牌管理 V2 - 数据迁移检查"
echo "=========================================="
echo ""

# 1. 检查现有设备数量
echo "[1] 检查现有设备数量..."
device_count=$(curl -sS -H "Authorization: Bearer $TOKEN" \
  "${BASE_URL}/api/v1/badge-devices?page=1&page_size=1" | \
  jq -r '.total // 0')

echo "   现有设备数量: $device_count"
echo ""

if [ "$device_count" -eq 0 ]; then
  echo "✅ 数据库为空，可以直接使用导入功能添加新设备"
  echo ""
  echo "使用方式："
  echo "1. 访问前端页面：http://localhost:3002/badges-v2/inventory"
  echo "2. 点击「导入设备」按钮"
  echo "3. 选择厂家，粘贴设备列表"
  echo "4. 点击「开始导入」"
  echo ""
  echo "或者使用 API 导入："
  echo "curl -H \"Authorization: Bearer \$TOKEN\" \\"
  echo "  -X POST \"${BASE_URL}/api/v1/badge-devices/actions/import\" \\"
  echo "  -H \"Content-Type: application/json\" \\"
  echo "  -d '{"
  echo "    \"manufacturer_code\": \"xiaomi\","
  echo "    \"manufacturer_name\": \"小米\","
  echo "    \"devices\": ["
  echo "      {\"device_no\": \"XM001\", \"hardware_model\": \"X1\"},"
  echo "      {\"device_no\": \"XM002\", \"hardware_model\": \"X1\"}"
  echo "    ]"
  echo "  }'"
  exit 0
fi

# 2. 检查设备状态分布
echo "[2] 检查设备状态分布..."
curl -sS -H "Authorization: Bearer $TOKEN" \
  "${BASE_URL}/api/v1/badge-devices?page=1&page_size=100" | \
  jq -r '.items[] | .status' | sort | uniq -c

echo ""

# 3. 检查是否需要状态映射
echo "[3] 检查状态值..."
echo "   V2 支持的状态："
echo "   - draft (草稿，刚导入)"
echo "   - available (可用，已验收)"
echo "   - in_use (使用中，已分配)"
echo "   - returned (已归还)"
echo "   - broken (故障)"
echo "   - scrapped (报废)"
echo ""

# 4. 获取厂家列表
echo "[4] 检查厂家配置..."
manufacturers=$(curl -sS -H "Authorization: Bearer $TOKEN" \
  "${BASE_URL}/api/v1/badge-devices/manufacturers" | jq -r '.items[]? | "\(.code) - \(.name)"')

if [ -z "$manufacturers" ]; then
  echo "   ⚠️  未配置厂家信息"
  echo "   需要在 badge_manufacturers 表中添加厂家数据"
else
  echo "   已配置的厂家："
  echo "$manufacturers" | sed 's/^/   - /'
fi

echo ""
echo "=========================================="
echo "迁移建议"
echo "=========================================="
echo ""

if [ "$device_count" -gt 0 ]; then
  echo "发现现有设备数据，建议："
  echo ""
  echo "1. 如果旧数据状态值不匹配 V2，需要执行 SQL 更新："
  echo ""
  echo "   -- 将旧状态映射到 V2 状态"
  echo "   UPDATE badge_devices SET status = CASE"
  echo "     WHEN status = 'pending_acceptance' THEN 'draft'"
  echo "     WHEN status = 'pending_assignment' THEN 'available'"
  echo "     WHEN status = 'in_use' THEN 'in_use'"
  echo "     WHEN status = 'maintenance' THEN 'broken'"
  echo "     WHEN status = 'retired' THEN 'scrapped'"
  echo "     ELSE 'available'"
  echo "   END"
  echo "   WHERE deleted_at IS NULL;"
  echo ""
  echo "2. 确保 health_status 字段有默认值："
  echo ""
  echo "   UPDATE badge_devices"
  echo "   SET health_status = 'unknown'"
  echo "   WHERE health_status IS NULL OR health_status = '';"
  echo ""
  echo "3. 确保 manufacturer_code 字段有值："
  echo ""
  echo "   UPDATE badge_devices"
  echo "   SET manufacturer_code = 'default'"
  echo "   WHERE manufacturer_code IS NULL OR manufacturer_code = '';"
  echo ""
fi

echo "=========================================="
echo "完成"
echo "=========================================="
