#!/usr/bin/env python3
"""
生成 Markdown 格式的 API 端点文档
从代码中提取所有端点并生成易读的 Markdown 文档
"""

import os
import re
from pathlib import Path
from typing import Dict, List, Tuple

# 端点描述映射
ENDPOINT_DESCRIPTIONS = {
    # 认证相关
    "Login": "用户登录，返回JWT令牌",
    "GetMe": "获取当前登录用户信息",
    "ChangePassword": "修改当前用户密码",
    "Captcha": "生成图形验证码",
    "SendSMS": "发送短信验证码",

    # CRUD操作
    "List": "获取列表（支持分页和筛选）",
    "Get": "获取详细信息",
    "Create": "创建新记录",
    "Update": "更新记录信息",
    "Delete": "删除记录（软删除）",

    # 其他操作
    "Sync": "同步数据",
    "Import": "导入数据",
    "Export": "导出数据",
    "Statistics": "获取统计数据",
    "Dashboard": "获取看板数据",
    "Summary": "获取汇总信息",
}

def extract_routes_from_file(file_path: str) -> List[Tuple[str, str, str]]:
    """从 handler.go 文件中提取路由定义"""
    routes = []
    with open(file_path, 'r', encoding='utf-8') as f:
        content = f.read()

    # 匹配路由定义
    pattern = r'mux\.Handle\("(GET|POST|PUT|DELETE|PATCH)\s+([^"]+)",\s+authMw\(http\.HandlerFunc\(h\.(\w+)\)\)\)'
    matches = re.findall(pattern, content)

    for method, path, handler_name in matches:
        routes.append((method, path, handler_name))

    return routes

def get_handler_description(handler_name: str, path: str) -> str:
    """根据handler名称生成描述"""
    # 尝试从预定义描述中匹配
    for key, desc in ENDPOINT_DESCRIPTIONS.items():
        if key in handler_name:
            return desc

    # 根据路径推断
    if "login" in path.lower():
        return "用户登录认证"
    elif "list" in handler_name.lower() or path.endswith("s"):
        return "获取列表数据（支持分页）"
    elif "create" in handler_name.lower():
        return "创建新记录"
    elif "update" in handler_name.lower():
        return "更新记录信息"
    elif "delete" in handler_name.lower():
        return "删除记录"
    elif "get" in handler_name.lower():
        return "获取详细信息"

    return f"执行 {handler_name} 操作"

def scan_all_handlers(project_root: str) -> Dict[str, List[Tuple[str, str, str]]]:
    """扫描所有模块的 handler.go 文件"""
    internal_dir = Path(project_root) / "internal"
    all_routes = {}

    for module_dir in internal_dir.iterdir():
        if not module_dir.is_dir():
            continue

        handler_files = list(module_dir.glob("handler*.go"))
        if not handler_files:
            continue

        module_name = module_dir.name
        module_routes = []

        for handler_file in handler_files:
            routes = extract_routes_from_file(str(handler_file))
            module_routes.extend(routes)

        if module_routes:
            all_routes[module_name] = module_routes

    return all_routes

def generate_markdown_docs(project_root: str):
    """生成 Markdown 格式的端点文档"""
    print("🚀 开始生成 Markdown 格式的端点文档...")

    # 扫描所有路由
    all_routes = scan_all_handlers(project_root)
    total_endpoints = sum(len(routes) for routes in all_routes.values())
    print(f"📊 发现 {total_endpoints} 个端点，分布在 {len(all_routes)} 个模块中")

    # 生成 Markdown 内容
    md_content = []
    md_content.append("# 灵策医疗运营系统 API 端点文档\n")
    md_content.append(f"**版本**: 1.0.0  \n")
    md_content.append(f"**端点总数**: {total_endpoints}  \n")
    md_content.append(f"**模块数量**: {len(all_routes)}  \n")
    md_content.append(f"**生成时间**: {Path(__file__).stat().st_mtime}\n")
    md_content.append("\n---\n")

    # 目录
    md_content.append("\n## 目录\n")
    for module, routes in sorted(all_routes.items()):
        md_content.append(f"- [{module}](#{module}) ({len(routes)} 个端点)\n")

    md_content.append("\n---\n")

    # 认证说明
    md_content.append("\n## 认证方式\n")
    md_content.append("\n所有需要认证的接口都需要在请求头中携带 JWT 令牌：\n")
    md_content.append("\n```http\n")
    md_content.append("Authorization: Bearer <your-token>\n")
    md_content.append("```\n")

    md_content.append("\n### 用户类型\n")
    md_content.append("\n- **admin**: 运维管理员，可访问所有租户数据\n")
    md_content.append("- **employee**: 机构员工（Web端），只能访问自己租户的数据\n")
    md_content.append("- **mobile**: 移动端用户，与employee权限相同但会话独立\n")

    md_content.append("\n---\n")

    # 各模块端点
    for module, routes in sorted(all_routes.items()):
        print(f"  📦 {module}: {len(routes)} 个端点")

        md_content.append(f"\n## {module}\n")
        md_content.append(f"\n**端点数量**: {len(routes)}\n")
        md_content.append("\n")

        # 按路径排序
        sorted_routes = sorted(routes, key=lambda x: x[1])

        for method, path, handler_name in sorted_routes:
            description = get_handler_description(handler_name, path)

            md_content.append(f"### `{method} {path}`\n")
            md_content.append(f"\n**描述**: {description}\n")
            md_content.append(f"\n**Handler**: `{handler_name}`\n")

            # 路径参数
            if "{" in path:
                params = re.findall(r'\{(\w+)\}', path)
                md_content.append(f"\n**路径参数**:\n")
                for param in params:
                    param_type = "integer" if param.endswith("_id") or param == "id" else "string"
                    md_content.append(f"- `{param}` ({param_type}): {param}参数\n")

            # 查询参数（列表接口）
            if "List" in handler_name or "list" in path.lower():
                md_content.append(f"\n**查询参数**:\n")
                md_content.append(f"- `page` (integer): 页码，默认 1\n")
                md_content.append(f"- `page_size` (integer): 每页记录数，默认 20\n")

            # 请求体示例
            if method in ["POST", "PUT", "PATCH"]:
                md_content.append(f"\n**请求体示例**:\n")
                md_content.append(f"```json\n")
                if "Login" in handler_name:
                    md_content.append('{\n')
                    md_content.append('  "username": "admin",\n')
                    md_content.append('  "password": "password123"\n')
                    md_content.append('}\n')
                elif "ChangePassword" in handler_name:
                    md_content.append('{\n')
                    md_content.append('  "old_password": "old_password",\n')
                    md_content.append('  "new_password": "new_password123"\n')
                    md_content.append('}\n')
                elif "Create" in handler_name or "Update" in handler_name:
                    md_content.append('{\n')
                    md_content.append('  "name": "示例名称",\n')
                    md_content.append('  "description": "示例描述"\n')
                    md_content.append('}\n')
                else:
                    md_content.append('{\n')
                    md_content.append('  "data": "请求数据"\n')
                    md_content.append('}\n')
                md_content.append(f"```\n")

            # 响应示例
            md_content.append(f"\n**响应示例**:\n")
            md_content.append(f"```json\n")
            if "Login" in handler_name:
                md_content.append('{\n')
                md_content.append('  "data": {\n')
                md_content.append('    "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",\n')
                md_content.append('    "user": {\n')
                md_content.append('      "id": 1,\n')
                md_content.append('      "username": "admin",\n')
                md_content.append('      "user_type": "admin"\n')
                md_content.append('    }\n')
                md_content.append('  }\n')
                md_content.append('}\n')
            elif "List" in handler_name:
                md_content.append('{\n')
                md_content.append('  "items": [\n')
                md_content.append('    {"id": 1, "name": "示例1"},\n')
                md_content.append('    {"id": 2, "name": "示例2"}\n')
                md_content.append('  ],\n')
                md_content.append('  "total": 100,\n')
                md_content.append('  "page": 1,\n')
                md_content.append('  "page_size": 20\n')
                md_content.append('}\n')
            elif "Get" in handler_name:
                md_content.append('{\n')
                md_content.append('  "data": {\n')
                md_content.append('    "id": 1,\n')
                md_content.append('    "name": "示例名称",\n')
                md_content.append('    "created_at": "2024-01-01T00:00:00Z"\n')
                md_content.append('  }\n')
                md_content.append('}\n')
            else:
                md_content.append('{\n')
                md_content.append('  "data": {\n')
                md_content.append('    "message": "操作成功"\n')
                md_content.append('  }\n')
                md_content.append('}\n')
            md_content.append(f"```\n")

            md_content.append("\n---\n")

    # 保存文档
    output_file = Path(project_root) / "docs" / "API_ENDPOINTS.md"
    with open(output_file, 'w', encoding='utf-8') as f:
        f.write(''.join(md_content))

    print(f"\n✅ 成功生成 Markdown 格式的端点文档!")
    print(f"📄 文件位置: {output_file}")
    print(f"📊 共生成 {total_endpoints} 个端点")

if __name__ == "__main__":
    project_root = Path(__file__).parent.parent
    generate_markdown_docs(str(project_root))
