#!/usr/bin/env python3
"""
增强版 OpenAPI 文档生成器
从代码中提取端点并生成详细的 OpenAPI 文档，包含完整的请求/响应示例
"""

import os
import re
import yaml
from pathlib import Path
from typing import Dict, List, Tuple, Optional

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

# 请求体示例
REQUEST_BODY_EXAMPLES = {
    "Login": {
        "username": "admin",
        "password": "password123"
    },
    "ChangePassword": {
        "old_password": "old_password",
        "new_password": "new_password123"
    },
    "Create": {
        "name": "示例名称",
        "description": "示例描述"
    },
    "Update": {
        "name": "更新后的名称",
        "description": "更新后的描述"
    }
}

# 响应示例
RESPONSE_EXAMPLES = {
    "Login": {
        "data": {
            "token": "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9...",
            "user": {
                "id": 1,
                "username": "admin",
                "user_type": "admin"
            }
        }
    },
    "List": {
        "items": [
            {"id": 1, "name": "示例1"},
            {"id": 2, "name": "示例2"}
        ],
        "total": 100,
        "page": 1,
        "page_size": 20
    },
    "Get": {
        "data": {
            "id": 1,
            "name": "示例名称",
            "created_at": "2024-01-01T00:00:00Z"
        }
    },
    "Create": {
        "data": {
            "id": 1,
            "message": "创建成功"
        }
    },
    "Update": {
        "data": {
            "message": "更新成功"
        }
    },
    "Delete": {
        "data": {
            "message": "删除成功"
        }
    }
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

def get_request_body_example(handler_name: str, method: str) -> Optional[Dict]:
    """获取请求体示例"""
    if method not in ["POST", "PUT", "PATCH"]:
        return None

    for key, example in REQUEST_BODY_EXAMPLES.items():
        if key in handler_name:
            return example

    # 默认示例
    return {"data": "请求数据"}

def get_response_example(handler_name: str) -> Dict:
    """获取响应示例"""
    for key, example in RESPONSE_EXAMPLES.items():
        if key in handler_name:
            return example

    # 默认成功响应
    return {"data": {"message": "操作成功"}}

def generate_operation(method: str, path: str, handler_name: str, module: str) -> Dict:
    """生成详细的操作定义"""
    description = get_handler_description(handler_name, path)

    operation = {
        "summary": handler_name,
        "description": f"{description}\n\n**所属模块**: {module}",
        "tags": [module],
        "responses": {
            "200": {
                "description": "请求成功",
                "content": {
                    "application/json": {
                        "schema": {
                            "type": "object"
                        },
                        "example": get_response_example(handler_name)
                    }
                }
            },
            "400": {
                "$ref": "#/components/responses/BadRequestError"
            },
            "401": {
                "$ref": "#/components/responses/UnauthorizedError"
            },
            "500": {
                "$ref": "#/components/responses/InternalServerError"
            }
        }
    }

    # 添加路径参数
    if "{" in path:
        params = re.findall(r'\{(\w+)\}', path)
        operation["parameters"] = []
        for param in params:
            param_type = "integer" if param.endswith("_id") or param == "id" else "string"
            param_example = 1 if param_type == "integer" else "example_value"
            operation["parameters"].append({
                "name": param,
                "in": "path",
                "required": True,
                "description": f"{param}参数",
                "schema": {
                    "type": param_type
                },
                "example": param_example
            })

    # 添加查询参数（列表接口）
    if "List" in handler_name or "list" in path.lower():
        if "parameters" not in operation:
            operation["parameters"] = []
        operation["parameters"].extend([
            {
                "name": "page",
                "in": "query",
                "description": "页码",
                "schema": {
                    "type": "integer",
                    "default": 1
                },
                "example": 1
            },
            {
                "name": "page_size",
                "in": "query",
                "description": "每页记录数",
                "schema": {
                    "type": "integer",
                    "default": 20
                },
                "example": 20
            }
        ])

    # 添加请求体
    if method in ["POST", "PUT", "PATCH"]:
        example = get_request_body_example(handler_name, method)
        if example:
            operation["requestBody"] = {
                "required": True,
                "description": "请求体",
                "content": {
                    "application/json": {
                        "schema": {
                            "type": "object"
                        },
                        "example": example
                    }
                }
            }

    return {method.lower(): operation}

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

def generate_enhanced_openapi(project_root: str):
    """生成增强版 OpenAPI 文档"""
    print("🚀 开始生成增强版 OpenAPI 文档...")

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

            operation = generate_operation(method, path, handler_name, module)
            doc["paths"][path].update(operation)

    # 保存文档
    output_file = Path(project_root) / "docs" / "openapi_complete.yaml"
    with open(output_file, 'w', encoding='utf-8') as f:
        yaml.dump(doc, f, allow_unicode=True, sort_keys=False,
                 default_flow_style=False, width=1000)

    print(f"\n✅ 成功生成增强版 OpenAPI 文档!")
    print(f"📄 文件位置: {output_file}")
    print(f"📊 共生成 {total_endpoints} 个端点")
    print(f"\n🎯 文档特性:")
    print(f"  ✅ 详细的端点描述")
    print(f"  ✅ 完整的请求示例")
    print(f"  ✅ 完整的响应示例")
    print(f"  ✅ 路径参数说明")
    print(f"  ✅ 查询参数说明")
    print(f"\n🚀 使用方法:")
    print(f"  cd {project_root}")
    print(f"  make docs")

if __name__ == "__main__":
    project_root = Path(__file__).parent.parent
    generate_enhanced_openapi(str(project_root))
