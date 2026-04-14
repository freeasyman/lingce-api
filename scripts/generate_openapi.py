#!/usr/bin/env python3
"""
OpenAPI 文档生成器
自动从代码中提取API端点信息并生成完整的OpenAPI 3.0规范文档
"""

import os
import re
import yaml
from pathlib import Path
from typing import Dict, List, Any

# 端点定义
ENDPOINTS = {
    "认证": [
        ("POST", "/api/v1/auth/login", "运维管理员登录", "使用用户名密码登录，获取JWT令牌", False),
        ("POST", "/api/v1/auth/login/institution", "机构员工登录", "机构员工使用用户名密码登录（Web端）", False),
        ("POST", "/api/v1/auth/login/mobile", "移动端登录", "移动端用户登录，会话与Web端独立", False),
        ("GET", "/api/v1/auth/me", "获取当前用户信息", "获取当前登录用户的详细信息", True),
        ("POST", "/api/v1/auth/change-password", "修改密码", "修改当前用户密码，修改后旧令牌失效", True),
        ("GET", "/api/v1/auth/captcha", "生成图形验证码", "生成图形验证码，返回验证码ID和Base64图片", False),
        ("POST", "/api/v1/auth/sms/send", "发送短信验证码", "向指定手机号发送短信验证码", False),
        ("POST", "/api/v1/auth/sms/login", "短信验证码登录", "使用手机号和短信验证码登录", False),
    ],
    "组织架构-机构": [
        ("GET", "/api/v1/tenants", "机构列表", "获取机构列表（分页）", True, "admin"),
        ("GET", "/api/v1/tenants/{id}", "机构详情", "获取指定机构的详细信息", True, "admin"),
        ("POST", "/api/v1/tenants", "创建机构", "创建新机构", True, "admin"),
        ("PUT", "/api/v1/tenants/{id}", "更新机构", "更新机构信息", True, "admin"),
        ("DELETE", "/api/v1/tenants/{id}", "删除机构", "软删除机构", True, "admin"),
    ],
    "组织架构-部门": [
        ("GET", "/api/v1/departments", "部门列表", "获取部门列表（分页）", True),
        ("GET", "/api/v1/departments/{id}", "部门详情", "获取指定部门的详细信息", True),
        ("POST", "/api/v1/departments", "创建部门", "创建新部门", True, "admin"),
        ("PUT", "/api/v1/departments/{id}", "更新部门", "更新部门信息", True, "admin"),
        ("DELETE", "/api/v1/departments/{id}", "删除部门", "软删除部门", True, "admin"),
        ("GET", "/api/v1/departments/health", "部门健康检查", "检查部门模块健康状态", True),
        ("POST", "/api/v1/departments/sync-from-visits", "从就诊同步部门", "从就诊记录同步部门信息", True, "admin"),
        ("GET", "/api/v1/departments/{id}/performance", "部门业绩统计", "获取部门业绩统计数据", True),
    ],
    "组织架构-员工": [
        ("GET", "/api/v1/employees", "员工列表", "获取员工列表（分页）", True),
        ("GET", "/api/v1/employees/{id}", "员工详情", "获取指定员工的详细信息", True),
        ("POST", "/api/v1/employees", "创建员工", "创建新员工", True, "admin"),
        ("PUT", "/api/v1/employees/{id}", "更新员工", "更新员工信息", True, "admin"),
        ("POST", "/api/v1/employees/{id}/reset-password", "重置密码", "重置员工密码", True, "admin"),
        ("DELETE", "/api/v1/employees/{id}", "删除员工", "软删除员工", True, "admin"),
    ],
    "组织架构-医生": [
        ("GET", "/api/v1/doctors/health", "医生健康检查", "检查医生模块健康状态", True),
        ("GET", "/api/v1/doctors", "医生列表", "获取医生列表（分页）", True),
        ("GET", "/api/v1/doctors/{id}", "医生详情", "获取指定医生的详细信息", True),
        ("POST", "/api/v1/doctors", "创建医生", "创建新医生", True, "admin"),
        ("PUT", "/api/v1/doctors/{id}", "更新医生", "更新医生信息", True, "admin"),
        ("DELETE", "/api/v1/doctors/{id}", "删除医生", "软删除医生", True, "admin"),
        ("POST", "/api/v1/doctors/sync-from-visits", "从就诊同步医生", "从就诊记录同步医生信息", True, "admin"),
        ("GET", "/api/v1/doctors/{id}/performance", "医生业绩统计", "获取医生业绩统计数据", True),
        ("GET", "/api/v1/doctors/performance/summary", "医生业绩汇总", "获取所有医生业绩汇总", True),
        ("GET", "/api/v1/doctors/{id}/employees", "医生映射员工", "获取医生关联的员工列表", True),
        ("POST", "/api/v1/doctors/{id}/employees", "更新医生员工映射", "更新医生与员工的映射关系", True, "admin"),
    ],
    "组织架构-患者": [
        ("GET", "/api/v1/patients", "患者列表", "获取患者列表（分页）", True),
        ("POST", "/api/v1/patients/sync-from-visits", "从就诊同步患者", "从就诊记录同步患者信息", True, "admin"),
        ("GET", "/api/v1/patients/{id}", "患者详情", "获取指定患者的详细信息", True),
        ("POST", "/api/v1/patients", "创建患者", "创建新患者", True),
        ("PUT", "/api/v1/patients/{id}", "更新患者", "更新患者信息", True),
        ("DELETE", "/api/v1/patients/{id}", "删除患者", "软删除患者", True, "admin"),
        ("GET", "/api/v1/patients/{id}/360", "患者360度视图", "获取患者完整信息视图（包含就诊、录音、病历等）", True),
    ],
}

def generate_path_item(method: str, path: str, summary: str, description: str,
                       requires_auth: bool = True, required_role: str = None) -> Dict[str, Any]:
    """生成单个路径项"""
    operation = {
        "summary": summary,
        "description": description,
        "responses": {
            "200": {
                "description": "成功",
                "content": {
                    "application/json": {
                        "schema": {
                            "type": "object",
                            "properties": {
                                "data": {
                                    "type": "object"
                                }
                            }
                        }
                    }
                }
            },
            "400": {"$ref": "#/components/responses/BadRequestError"},
            "500": {"$ref": "#/components/responses/InternalServerError"}
        }
    }

    # 添加认证要求
    if requires_auth:
        operation["responses"]["401"] = {"$ref": "#/components/responses/UnauthorizedError"}
        if required_role:
            operation["responses"]["403"] = {"$ref": "#/components/responses/ForbiddenError"}
            operation["description"] += f"\n\n**权限要求**: {required_role}"
    else:
        operation["security"] = []

    # 添加路径参数
    if "{" in path:
        params = re.findall(r'\{(\w+)\}', path)
        operation["parameters"] = []
        for param in params:
            operation["parameters"].append({
                "name": param,
                "in": "path",
                "required": True,
                "schema": {"type": "integer" if param.endswith("_id") or param == "id" else "string"},
                "description": f"{param}参数"
            })

    # 添加请求体（POST/PUT方法）
    if method in ["POST", "PUT", "PATCH"]:
        operation["requestBody"] = {
            "required": True,
            "content": {
                "application/json": {
                    "schema": {
                        "type": "object",
                        "description": "请求体"
                    }
                }
            }
        }

    # 添加查询参数（GET方法的列表接口）
    if method == "GET" and not "{" in path and "list" not in path.lower():
        if any(keyword in path for keyword in ["/api/v1/tenants", "/api/v1/departments",
                                                "/api/v1/employees", "/api/v1/doctors",
                                                "/api/v1/patients"]):
            operation["parameters"] = [
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
            ]

    return {method.lower(): operation}

def generate_openapi_doc():
    """生成完整的OpenAPI文档"""
    print("正在生成OpenAPI文档...")

    # 读取基础模板
    base_file = Path(__file__).parent / "openapi.yaml"
    if base_file.exists():
        with open(base_file, 'r', encoding='utf-8') as f:
            doc = yaml.safe_load(f)
    else:
        print("错误: 找不到基础模板文件 openapi.yaml")
        return

    # 添加所有端点
    paths_added = 0
    for tag, endpoints in ENDPOINTS.items():
        for endpoint in endpoints:
            method, path, summary, description = endpoint[:4]
            requires_auth = endpoint[4] if len(endpoint) > 4 else True
            required_role = endpoint[5] if len(endpoint) > 5 else None

            if path not in doc["paths"]:
                doc["paths"][path] = {}

            path_item = generate_path_item(method, path, summary, description,
                                          requires_auth, required_role)
            doc["paths"][path].update(path_item)

            # 添加标签
            if method.lower() in doc["paths"][path]:
                doc["paths"][path][method.lower()]["tags"] = [tag]

            paths_added += 1

    # 保存文档
    output_file = Path(__file__).parent / "openapi_generated.yaml"
    with open(output_file, 'w', encoding='utf-8') as f:
        yaml.dump(doc, f, allow_unicode=True, sort_keys=False, default_flow_style=False)

    print(f"✅ 成功生成OpenAPI文档: {output_file}")
    print(f"📊 共添加 {paths_added} 个端点")
    print(f"\n使用方法:")
    print(f"1. 安装 Swagger UI: npm install -g swagger-ui-watcher")
    print(f"2. 启动文档服务: swagger-ui-watcher {output_file}")
    print(f"3. 在浏览器中打开: http://localhost:8000")

if __name__ == "__main__":
    generate_openapi_doc()
