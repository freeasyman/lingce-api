#!/usr/bin/env python3
"""
从代码中提取端点并生成完整的 OpenAPI 文档
扫描所有 handler.go 文件，自动提取路由定义
"""

import os
import re
import yaml
from pathlib import Path
from typing import Dict, List, Tuple

def extract_routes_from_file(file_path: str) -> List[Tuple[str, str, str]]:
    """从 handler.go 文件中提取路由定义"""
    routes = []

    with open(file_path, 'r', encoding='utf-8') as f:
        content = f.read()

    # 匹配路由定义: mux.Handle("METHOD /path", ...)
    pattern = r'mux\.Handle\("(GET|POST|PUT|DELETE|PATCH)\s+([^"]+)",\s+authMw\(http\.HandlerFunc\(h\.(\w+)\)\)\)'
    matches = re.findall(pattern, content)

    for method, path, handler_name in matches:
        routes.append((method, path, handler_name))

    return routes

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

def generate_operation_from_route(method: str, path: str, handler_name: str,
                                 module: str) -> Dict[str, any]:
    """根据路由信息生成操作定义"""
    # 从handler名称推断功能
    summary = handler_name.replace("Get", "获取").replace("List", "列表").replace(
        "Create", "创建").replace("Update", "更新").replace("Delete", "删除")

    operation = {
        "summary": summary,
        "description": f"{module}模块 - {summary}",
        "tags": [module],
        "responses": {
            "200": {
                "description": "成功",
                "content": {
                    "application/json": {
                        "schema": {"type": "object"}
                    }
                }
            },
            "400": {"$ref": "#/components/responses/BadRequestError"},
            "401": {"$ref": "#/components/responses/UnauthorizedError"},
            "500": {"$ref": "#/components/responses/InternalServerError"}
        }
    }

    # 添加路径参数
    if "{" in path:
        params = re.findall(r'\{(\w+)\}', path)
        operation["parameters"] = []
        for param in params:
            param_type = "integer" if param.endswith("_id") or param == "id" else "string"
            operation["parameters"].append({
                "name": param,
                "in": "path",
                "required": True,
                "schema": {"type": param_type}
            })

    # 添加分页参数（列表接口）
    if "List" in handler_name or "list" in path.lower():
        if "parameters" not in operation:
            operation["parameters"] = []
        operation["parameters"].extend([
            {"name": "page", "in": "query", "schema": {"type": "integer", "default": 1}},
            {"name": "page_size", "in": "query", "schema": {"type": "integer", "default": 20}}
        ])

    # 添加请求体（POST/PUT/PATCH）
    if method in ["POST", "PUT", "PATCH"]:
        operation["requestBody"] = {
            "required": True,
            "content": {
                "application/json": {
                    "schema": {"type": "object"}
                }
            }
        }

    return {method.lower(): operation}

def generate_complete_openapi(project_root: str):
    """生成完整的 OpenAPI 文档"""
    print("🔍 扫描项目中的所有端点...")

    # 扫描所有路由
    all_routes = scan_all_handlers(project_root)

    total_endpoints = sum(len(routes) for routes in all_routes.values())
    print(f"📊 发现 {total_endpoints} 个端点，分布在 {len(all_routes)} 个模块中")

    # 读取基础模板
    base_file = Path(project_root) / "docs" / "openapi.yaml"
    with open(base_file, 'r', encoding='utf-8') as f:
        doc = yaml.safe_load(f)

    # 清空现有paths
    doc["paths"] = {}

    # 生成所有端点
    for module, routes in sorted(all_routes.items()):
        print(f"  📦 {module}: {len(routes)} 个端点")
        for method, path, handler_name in routes:
            if path not in doc["paths"]:
                doc["paths"][path] = {}

            operation = generate_operation_from_route(method, path, handler_name, module)
            doc["paths"][path].update(operation)

    # 保存文档
    output_file = Path(project_root) / "docs" / "openapi_complete.yaml"
    with open(output_file, 'w', encoding='utf-8') as f:
        yaml.dump(doc, f, allow_unicode=True, sort_keys=False,
                 default_flow_style=False, width=1000)

    print(f"\n✅ 成功生成完整的 OpenAPI 文档!")
    print(f"📄 文件位置: {output_file}")
    print(f"📊 共生成 {total_endpoints} 个端点")
    print(f"\n🚀 使用方法:")
    print(f"  cd {project_root}")
    print(f"  swagger-ui-watcher docs/openapi_complete.yaml")
    print(f"  或")
    print(f"  make docs")

if __name__ == "__main__":
    project_root = Path(__file__).parent.parent
    generate_complete_openapi(str(project_root))
