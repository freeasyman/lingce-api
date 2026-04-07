#!/bin/bash

# lingce-api 快速测试脚本

API_URL="http://localhost:18080"

echo "========================================="
echo "  lingce-api 快速测试"
echo "========================================="
echo ""

# 1. 健康检查
echo "📋 1. 健康检查..."
HEALTH=$(curl -s $API_URL/healthz)
if echo "$HEALTH" | grep -q "ok"; then
  echo "✅ 服务运行正常"
  echo "   $HEALTH"
else
  echo "❌ 服务未运行或异常"
  echo "   请先启动服务: make run"
  exit 1
fi
echo ""

# 2. 登录测试
echo "🔐 2. 测试登录..."
echo "   请输入管理员用户名（默认: admin）:"
read -p "   Username: " USERNAME
USERNAME=${USERNAME:-admin}

echo "   请输入密码:"
read -sp "   Password: " PASSWORD
echo ""

LOGIN_RESPONSE=$(curl -s -X POST $API_URL/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d "{\"username\":\"$USERNAME\",\"password\":\"$PASSWORD\"}")

TOKEN=$(echo $LOGIN_RESPONSE | grep -o '"token":"[^"]*"' | cut -d'"' -f4)

if [ -n "$TOKEN" ] && [ "$TOKEN" != "null" ]; then
  echo "✅ 登录成功"
  echo "   Token: ${TOKEN:0:50}..."
  echo ""
else
  echo "❌ 登录失败"
  echo "   响应: $LOGIN_RESPONSE"
  echo ""
  echo "💡 提示："
  echo "   1. 确认数据库中有管理员账号"
  echo "   2. 检查用户名和密码是否正确"
  echo "   3. 查看服务日志获取详细错误信息"
  exit 1
fi

# 3. 获取当前用户
echo "👤 3. 获取当前用户信息..."
ME_RESPONSE=$(curl -s -X GET $API_URL/api/v1/auth/me \
  -H "Authorization: Bearer $TOKEN")

if echo "$ME_RESPONSE" | grep -q "data"; then
  echo "✅ 获取成功"
  echo "$ME_RESPONSE" | python3 -m json.tool 2>/dev/null || echo "$ME_RESPONSE"
else
  echo "❌ 获取失败"
  echo "   响应: $ME_RESPONSE"
fi
echo ""

# 4. 测试机构列表
echo "🏢 4. 测试机构列表..."
TENANTS_RESPONSE=$(curl -s -X GET "$API_URL/api/v1/tenants?page=1&page_size=5" \
  -H "Authorization: Bearer $TOKEN")

if echo "$TENANTS_RESPONSE" | grep -q "items"; then
  TENANT_COUNT=$(echo "$TENANTS_RESPONSE" | grep -o '"total":[0-9]*' | cut -d':' -f2)
  echo "✅ 获取成功，共 $TENANT_COUNT 个机构"
else
  echo "⚠️  获取失败或无数据"
fi
echo ""

# 5. 测试部门列表
echo "🏛️  5. 测试部门列表..."
DEPTS_RESPONSE=$(curl -s -X GET "$API_URL/api/v1/departments?page=1&page_size=5" \
  -H "Authorization: Bearer $TOKEN")

if echo "$DEPTS_RESPONSE" | grep -q "items"; then
  DEPT_COUNT=$(echo "$DEPTS_RESPONSE" | grep -o '"total":[0-9]*' | cut -d':' -f2)
  echo "✅ 获取成功，共 $DEPT_COUNT 个部门"
else
  echo "⚠️  获取失败或无数据"
fi
echo ""

# 6. 测试员工列表
echo "👥 6. 测试员工列表..."
EMPS_RESPONSE=$(curl -s -X GET "$API_URL/api/v1/employees?page=1&page_size=5" \
  -H "Authorization: Bearer $TOKEN")

if echo "$EMPS_RESPONSE" | grep -q "items"; then
  EMP_COUNT=$(echo "$EMPS_RESPONSE" | grep -o '"total":[0-9]*' | cut -d':' -f2)
  echo "✅ 获取成功，共 $EMP_COUNT 个员工"
else
  echo "⚠️  获取失败或无数据"
fi
echo ""

# 总结
echo "========================================="
echo "  测试完成"
echo "========================================="
echo ""
echo "💡 下一步："
echo "   1. 查看完整 API 文档: make docs"
echo "   2. 访问 Swagger UI: http://localhost:8000"
echo "   3. 查看本地运行指南: docs/LOCAL_SETUP_GUIDE.md"
echo ""
