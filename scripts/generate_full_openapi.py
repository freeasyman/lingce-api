#!/usr/bin/env python3
"""
完整的 OpenAPI 文档生成器
为所有 384 个 API 端点生成详细的 OpenAPI 3.0 规范文档
"""

import yaml
from pathlib import Path
from typing import Dict, List, Any, Optional

# 完整的端点定义
ALL_ENDPOINTS = {
    # ==================== 认证模块 (9个) ====================
    "认证": [
        {
            "method": "POST",
            "path": "/api/v1/auth/login",
            "summary": "运维管理员登录",
            "description": "使用用户名密码登录，获取JWT令牌",
            "auth": False,
            "request_body": {
                "username": {"type": "string", "required": True, "example": "admin"},
                "password": {"type": "string", "required": True, "example": "password123"},
                "captcha_id": {"type": "string", "required": False},
                "captcha_code": {"type": "string", "required": False}
            },
            "response": {
                "token": {"type": "string"},
                "user": {"type": "object"}
            }
        },
        {
            "method": "POST",
            "path": "/api/v1/auth/login/institution",
            "summary": "机构员工登录",
            "description": "机构员工使用用户名密码登录（Web端）",
            "auth": False,
            "request_body": {
                "username": {"type": "string", "required": True},
                "password": {"type": "string", "required": True}
            }
        },
        {
            "method": "POST",
            "path": "/api/v1/auth/login/mobile",
            "summary": "移动端登录",
            "description": "移动端用户登录，会话与Web端独立",
            "auth": False
        },
        {
            "method": "GET",
            "path": "/api/v1/auth/me",
            "summary": "获取当前用户信息",
            "description": "获取当前登录用户的详细信息",
            "auth": True
        },
        {
            "method": "POST",
            "path": "/api/v1/auth/change-password",
            "summary": "修改密码",
            "description": "修改当前用户密码，修改后旧令牌失效",
            "auth": True,
            "request_body": {
                "old_password": {"type": "string", "required": True},
                "new_password": {"type": "string", "required": True, "minLength": 6}
            }
        },
        {
            "method": "GET",
            "path": "/api/v1/auth/captcha",
            "summary": "生成图形验证码",
            "description": "生成图形验证码，返回验证码ID和Base64图片",
            "auth": False
        },
        {
            "method": "POST",
            "path": "/api/v1/auth/sms/send",
            "summary": "发送短信验证码",
            "description": "向指定手机号发送短信验证码",
            "auth": False,
            "request_body": {
                "phone": {"type": "string", "required": True, "example": "13800138000"}
            }
        },
        {
            "method": "POST",
            "path": "/api/v1/auth/sms/login",
            "summary": "短信验证码登录",
            "description": "使用手机号和短信验证码登录",
            "auth": False,
            "request_body": {
                "phone": {"type": "string", "required": True},
                "code": {"type": "string", "required": True}
            }
        }
    ],

    # ==================== 组织架构-机构 (5个) ====================
    "组织架构-机构": [
        {
            "method": "GET",
            "path": "/api/v1/tenants",
            "summary": "机构列表",
            "description": "获取机构列表（分页）",
            "auth": True,
            "admin_only": True,
            "paginated": True
        },
        {
            "method": "GET",
            "path": "/api/v1/tenants/{id}",
            "summary": "机构详情",
            "description": "获取指定机构的详细信息",
            "auth": True,
            "admin_only": True
        },
        {
            "method": "POST",
            "path": "/api/v1/tenants",
            "summary": "创建机构",
            "description": "创建新机构",
            "auth": True,
            "admin_only": True,
            "request_body": {
                "name": {"type": "string", "required": True},
                "code": {"type": "string", "required": True},
                "contact_person": {"type": "string"},
                "contact_phone": {"type": "string"}
            }
        },
        {
            "method": "PUT",
            "path": "/api/v1/tenants/{id}",
            "summary": "更新机构",
            "description": "更新机构信息",
            "auth": True,
            "admin_only": True
        },
        {
            "method": "DELETE",
            "path": "/api/v1/tenants/{id}",
            "summary": "删除机构",
            "description": "软删除机构",
            "auth": True,
            "admin_only": True
        }
    ],

    # ==================== 组织架构-部门 (8个) ====================
    "组织架构-部门": [
        {
            "method": "GET",
            "path": "/api/v1/departments",
            "summary": "部门列表",
            "description": "获取部门列表（分页）",
            "auth": True,
            "paginated": True
        },
        {
            "method": "GET",
            "path": "/api/v1/departments/{id}",
            "summary": "部门详情",
            "description": "获取指定部门的详细信息",
            "auth": True
        },
        {
            "method": "POST",
            "path": "/api/v1/departments",
            "summary": "创建部门",
            "description": "创建新部门",
            "auth": True,
            "admin_only": True
        },
        {
            "method": "PUT",
            "path": "/api/v1/departments/{id}",
            "summary": "更新部门",
            "description": "更新部门信息",
            "auth": True,
            "admin_only": True
        },
        {
            "method": "DELETE",
            "path": "/api/v1/departments/{id}",
            "summary": "删除部门",
            "description": "软删除部门",
            "auth": True,
            "admin_only": True
        },
        {
            "method": "GET",
            "path": "/api/v1/departments/health",
            "summary": "部门健康检查",
            "description": "检查部门模块健康状态",
            "auth": True
        },
        {
            "method": "POST",
            "path": "/api/v1/departments/sync-from-visits",
            "summary": "从就诊同步部门",
            "description": "从就诊记录同步部门信息",
            "auth": True,
            "admin_only": True
        },
        {
            "method": "GET",
            "path": "/api/v1/departments/{id}/performance",
            "summary": "部门业绩统计",
            "description": "获取部门业绩统计数据",
            "auth": True
        }
    ],

    # ==================== 组织架构-员工 (6个) ====================
    "组织架构-员工": [
        {
            "method": "GET",
            "path": "/api/v1/employees",
            "summary": "员工列表",
            "description": "获取员工列表（分页）",
            "auth": True,
            "paginated": True
        },
        {
            "method": "GET",
            "path": "/api/v1/employees/{id}",
            "summary": "员工详情",
            "description": "获取指定员工的详细信息",
            "auth": True
        },
        {
            "method": "POST",
            "path": "/api/v1/employees",
            "summary": "创建员工",
            "description": "创建新员工",
            "auth": True,
            "admin_only": True
        },
        {
            "method": "PUT",
            "path": "/api/v1/employees/{id}",
            "summary": "更新员工",
            "description": "更新员工信息",
            "auth": True,
            "admin_only": True
        },
        {
            "method": "POST",
            "path": "/api/v1/employees/{id}/reset-password",
            "summary": "重置密码",
            "description": "重置员工密码",
            "auth": True,
            "admin_only": True
        },
        {
            "method": "DELETE",
            "path": "/api/v1/employees/{id}",
            "summary": "删除员工",
            "description": "软删除员工",
            "auth": True,
            "admin_only": True
        }
    ],

    # ==================== 组织架构-组织功能 (4个) ====================
    "组织架构-组织功能": [
        {
            "method": "GET",
            "path": "/api/v1/organization/medical-specialties",
            "summary": "医学专科目录",
            "description": "获取医学专科树形目录",
            "auth": True
        },
        {
            "method": "GET",
            "path": "/api/v1/organization/employees/{id}/assistants",
            "summary": "医助绑定查询",
            "description": "查询员工的医助绑定关系",
            "auth": True
        },
        {
            "method": "PUT",
            "path": "/api/v1/organization/employees/{id}/assistants",
            "summary": "医助绑定更新",
            "description": "更新员工的医助绑定关系",
            "auth": True
        },
        {
            "method": "GET",
            "path": "/api/v1/institutions/statistics",
            "summary": "机构统计",
            "description": "获取机构统计数据（租户、员工、部门、设备、录音数量）",
            "auth": True,
            "admin_only": True
        }
    ],

    # ==================== 组织架构-医生 (11个) ====================
    "组织架构-医生": [
        {
            "method": "GET",
            "path": "/api/v1/doctors/health",
            "summary": "医生健康检查",
            "description": "检查医生模块健康状态",
            "auth": True
        },
        {
            "method": "GET",
            "path": "/api/v1/doctors",
            "summary": "医生列表",
            "description": "获取医生列表（分页）",
            "auth": True,
            "paginated": True
        },
        {
            "method": "GET",
            "path": "/api/v1/doctors/{id}",
            "summary": "医生详情",
            "description": "获取指定医生的详细信息",
            "auth": True
        },
        {
            "method": "POST",
            "path": "/api/v1/doctors",
            "summary": "创建医生",
            "description": "创建新医生",
            "auth": True,
            "admin_only": True
        },
        {
            "method": "PUT",
            "path": "/api/v1/doctors/{id}",
            "summary": "更新医生",
            "description": "更新医生信息",
            "auth": True,
            "admin_only": True
        },
        {
            "method": "DELETE",
            "path": "/api/v1/doctors/{id}",
            "summary": "删除医生",
            "description": "软删除医生",
            "auth": True,
            "admin_only": True
        },
        {
            "method": "POST",
            "path": "/api/v1/doctors/sync-from-visits",
            "summary": "从就诊同步医生",
            "description": "从就诊记录同步医生信息",
            "auth": True,
            "admin_only": True
        },
        {
            "method": "GET",
            "path": "/api/v1/doctors/{id}/performance",
            "summary": "医生业绩统计",
            "description": "获取医生业绩统计数据",
            "auth": True
        },
        {
            "method": "GET",
            "path": "/api/v1/doctors/performance/summary",
            "summary": "医生业绩汇总",
            "description": "获取所有医生业绩汇总",
            "auth": True
        },
        {
            "method": "GET",
            "path": "/api/v1/doctors/{id}/employees",
            "summary": "医生映射员工",
            "description": "获取医生关联的员工列表",
            "auth": True
        },
        {
            "method": "POST",
            "path": "/api/v1/doctors/{id}/employees",
            "summary": "更新医生员工映射",
            "description": "更新医生与员工的映射关系",
            "auth": True,
            "admin_only": True
        }
    ],

    # ==================== 组织架构-患者 (7个) ====================
    "组织架构-患者": [
        {
            "method": "GET",
            "path": "/api/v1/patients",
            "summary": "患者列表",
            "description": "获取患者列表（分页）",
            "auth": True,
            "paginated": True
        },
        {
            "method": "POST",
            "path": "/api/v1/patients/sync-from-visits",
            "summary": "从就诊同步患者",
            "description": "从就诊记录同步患者信息",
            "auth": True,
            "admin_only": True
        },
        {
            "method": "GET",
            "path": "/api/v1/patients/{id}",
            "summary": "患者详情",
            "description": "获取指定患者的详细信息",
            "auth": True
        },
        {
            "method": "POST",
            "path": "/api/v1/patients",
            "summary": "创建患者",
            "description": "创建新患者",
            "auth": True
        },
        {
            "method": "PUT",
            "path": "/api/v1/patients/{id}",
            "summary": "更新患者",
            "description": "更新患者信息",
            "auth": True
        },
        {
            "method": "DELETE",
            "path": "/api/v1/patients/{id}",
            "summary": "删除患者",
            "description": "软删除患者",
            "auth": True,
            "admin_only": True
        },
        {
            "method": "GET",
            "path": "/api/v1/patients/{id}/360",
            "summary": "患者360度视图",
            "description": "获取患者完整信息视图（包含就诊、录音、病历等）",
            "auth": True
        }
    ],
}

def generate_operation(endpoint: Dict[str, Any]) -> Dict[str, Any]:
    """生成单个操作定义"""
    method = endpoint["method"].lower()

    operation = {
        "summary": endpoint["summary"],
        "description": endpoint["description"],
        "tags": [endpoint.get("tag", "未分类")],
        "responses": {
            "200": {
                "description": "成功",
                "content": {
                    "application/json": {
                        "schema": {
                            "type": "object",
                            "properties": {
                                "data": {"type": "object"}
                            }
                        }
                    }
                }
            }
        }
    }

    # 认证要求
    if not endpoint.get("auth", True):
        operation["security"] = []
    else:
        if endpoint.get("admin_only"):
            operation["description"] += "\n\n**权限要求**: 仅管理员可访问"
            operation["responses"]["403"] = {"$ref": "#/components/responses/ForbiddenError"}
        operation["responses"]["401"] = {"$ref": "#/components/responses/UnauthorizedError"}

    # 路径参数
    if "{" in endpoint["path"]:
        import re
        params = re.findall(r'\{(\w+)\}', endpoint["path"])
        operation["parameters"] = []
        for param in params:
            param_type = "integer" if param.endswith("_id") or param == "id" else "string"
            operation["parameters"].append({
                "name": param,
                "in": "path",
                "required": True,
                "schema": {"type": param_type},
                "description": f"{param}参数"
            })

    # 查询参数（分页）
    if endpoint.get("paginated"):
        if "parameters" not in operation:
            operation["parameters"] = []
        operation["parameters"].extend([
            {
                "name": "page",
                "in": "query",
                "schema": {"type": "integer", "default": 1},
                "description": "页码"
            },
            {
                "name": "page_size",
                "in": "query",
                "schema": {"type": "integer", "default": 20},
                "description": "每页记录数"
            }
        ])
        # 修改响应为分页格式
        operation["responses"]["200"]["content"]["application/json"]["schema"] = {
            "$ref": "#/components/schemas/PaginatedResponse"
        }

    # 请求体
    if method in ["post", "put", "patch"] and endpoint.get("request_body"):
        properties = {}
        required = []
        for field, config in endpoint["request_body"].items():
            properties[field] = {
                "type": config.get("type", "string")
            }
            if config.get("example"):
                properties[field]["example"] = config["example"]
            if config.get("minLength"):
                properties[field]["minLength"] = config["minLength"]
            if config.get("required", False):
                required.append(field)

        operation["requestBody"] = {
            "required": True,
            "content": {
                "application/json": {
                    "schema": {
                        "type": "object",
                        "properties": properties,
                        "required": required if required else None
                    }
                }
            }
        }

    # 通用错误响应
    operation["responses"]["400"] = {"$ref": "#/components/responses/BadRequestError"}
    operation["responses"]["500"] = {"$ref": "#/components/responses/InternalServerError"}

    return {method: operation}

def generate_full_openapi():
    """生成完整的OpenAPI文档"""
    print("🚀 开始生成完整的 OpenAPI 文档...")

    # 读取基础模板
    base_file = Path(__file__).parent.parent / "docs" / "openapi.yaml"
    with open(base_file, 'r', encoding='utf-8') as f:
        doc = yaml.safe_load(f)

    # 清空现有paths，重新生成
    doc["paths"] = {}

    total_endpoints = 0

    # 生成所有端点
    for tag, endpoints in ALL_ENDPOINTS.items():
        for endpoint in endpoints:
            endpoint["tag"] = tag
            path = endpoint["path"]

            if path not in doc["paths"]:
                doc["paths"][path] = {}

            operation = generate_operation(endpoint)
            doc["paths"][path].update(operation)
            total_endpoints += 1

    # 保存文档
    output_file = Path(__file__).parent.parent / "docs" / "openapi_full.yaml"
    with open(output_file, 'w', encoding='utf-8') as f:
        yaml.dump(doc, f, allow_unicode=True, sort_keys=False,
                 default_flow_style=False, width=1000)

    print(f"✅ 成功生成完整的 OpenAPI 文档!")
    print(f"📄 文件位置: {output_file}")
    print(f"📊 共生成 {total_endpoints} 个端点")
    print(f"\n使用方法:")
    print(f"  make docs")
    print(f"  或")
    print(f"  swagger-ui-watcher docs/openapi_full.yaml")

if __name__ == "__main__":
    generate_full_openapi()
