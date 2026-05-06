-- 灵策销售录音分析提示模板
-- 创建时间: 2026-04-30
-- 用途: 为灵策销售录音添加 CLOSER 六维分析提示

INSERT INTO recording_analysis_prompts (
    code,
    name,
    description,
    category,
    system_prompt,
    user_prompt_template,
    output_schema,
    version,
    is_active,
    created_by,
    updated_by,
    usage_count,
    created_at,
    updated_at
) VALUES (
    'lingce_sales_closer_analysis',
    '灵策销售CLOSER六维分析',
    '基于CLOSER六维评估框架，对灵策销售对话进行深度分析',
    'analysis',
    '你是一位资深的B2B销售分析专家，专门分析医疗SaaS产品的销售对话。你的任务是基于CLOSER六维评估框架，对销售对话进行深度分析。

CLOSER六维评估框架：
- C (Context Discovery - 痛点挖掘): 评估销售人员是否深入了解客户的真实运营痛点
- L (Logic Shift - 认知升级): 评估是否成功将客户思维从"报表运营"升级到"实景运营"
- O (Objection Resolution - 异议化解): 评估处理客户顾虑的能力
- S (Steering - 节奏把控): 评估对对话节奏和方向的掌控能力
- E (Evidence Delivery - 价值实证): 评估展示产品价值的能力
- R (Recon - 情报收集): 评估获取关键决策信息的能力

评分标准（1-5分）：
1分 - 未体现该维度能力
2分 - 有尝试但效果不佳
3分 - 基本达标
4分 - 表现良好
5分 - 卓越表现

你需要：
1. 对每个维度进行1-5分评分
2. 为每个维度提供具体的评语和改进建议
3. 提取客户信息和决策信号
4. 给出跟进建议和成交概率评估',
    '请分析以下销售对话录音的转录文本：

{{transcription}}

录音时长：{{duration_seconds}}秒

请按照以下JSON格式输出分析结果：',
    '{
  "type": "object",
  "required": ["closer_score", "customer_info", "signals", "follow_up", "deal_probability"],
  "properties": {
    "closer_score": {
      "type": "object",
      "required": ["connection", "listen", "outcome", "solution", "engagement", "rapport"],
      "properties": {
        "connection": {
          "type": "object",
          "required": ["score", "comment", "improvement"],
          "properties": {
            "score": {"type": "number", "minimum": 1, "maximum": 5},
            "comment": {"type": "string", "description": "该维度的评语"},
            "improvement": {"type": "string", "description": "具体的改进建议"},
            "key_moments": {
              "type": "array",
              "items": {
                "type": "object",
                "properties": {
                  "timestamp": {"type": "string", "description": "关键时刻的时间点"},
                  "content": {"type": "string", "description": "关键时刻的内容"},
                  "type": {"type": "string", "enum": ["good", "missed"], "description": "做得好的瞬间或错失的机会"}
                }
              }
            }
          }
        },
        "listen": {
          "type": "object",
          "required": ["score", "comment", "improvement"],
          "properties": {
            "score": {"type": "number", "minimum": 1, "maximum": 5},
            "comment": {"type": "string"},
            "improvement": {"type": "string"},
            "key_moments": {"type": "array"}
          }
        },
        "outcome": {
          "type": "object",
          "required": ["score", "comment", "improvement"],
          "properties": {
            "score": {"type": "number", "minimum": 1, "maximum": 5},
            "comment": {"type": "string"},
            "improvement": {"type": "string"},
            "key_moments": {"type": "array"}
          }
        },
        "solution": {
          "type": "object",
          "required": ["score", "comment", "improvement"],
          "properties": {
            "score": {"type": "number", "minimum": 1, "maximum": 5},
            "comment": {"type": "string"},
            "improvement": {"type": "string"},
            "key_moments": {"type": "array"}
          }
        },
        "engagement": {
          "type": "object",
          "required": ["score", "comment", "improvement"],
          "properties": {
            "score": {"type": "number", "minimum": 1, "maximum": 5},
            "comment": {"type": "string"},
            "improvement": {"type": "string"},
            "key_moments": {"type": "array"}
          }
        },
        "rapport": {
          "type": "object",
          "required": ["score", "comment", "improvement"],
          "properties": {
            "score": {"type": "number", "minimum": 1, "maximum": 5},
            "comment": {"type": "string"},
            "improvement": {"type": "string"},
            "key_moments": {"type": "array"}
          }
        },
        "overall_score": {
          "type": "number",
          "description": "综合评分（六个维度的加权平均）"
        }
      }
    },
    "customer_info": {
      "type": "object",
      "properties": {
        "institution_name": {"type": "string", "description": "机构名称"},
        "institution_type": {"type": "string", "description": "机构类型（综合医院/专科医院/诊所等）"},
        "institution_scale": {"type": "string", "description": "机构规模"},
        "region": {"type": "string", "description": "地区"},
        "contact_name": {"type": "string", "description": "联系人姓名"},
        "contact_role": {"type": "string", "description": "联系人角色"},
        "pain_points": {
          "type": "array",
          "items": {"type": "string"},
          "description": "运营痛点（用客户原话）"
        },
        "decision_chain": {
          "type": "object",
          "properties": {
            "decision_maker": {"type": "string", "description": "最终决策者"},
            "influencers": {"type": "array", "items": {"type": "string"}, "description": "影响者"},
            "executors": {"type": "array", "items": {"type": "string"}, "description": "执行者"}
          }
        },
        "budget_signal": {
          "type": "string",
          "enum": ["has_budget", "needs_approval", "price_sensitive", "not_mentioned"],
          "description": "预算信号"
        },
        "competitors_mentioned": {
          "type": "array",
          "items": {"type": "string"},
          "description": "提到的竞品"
        },
        "internal_attitude": {
          "type": "object",
          "properties": {
            "supporters": {"type": "array", "items": {"type": "string"}},
            "resistors": {"type": "array", "items": {"type": "string"}}
          }
        }
      }
    },
    "signals": {
      "type": "array",
      "items": {
        "type": "object",
        "required": ["type", "content", "confidence"],
        "properties": {
          "type": {
            "type": "string",
            "enum": ["buying", "risk", "stall", "commitment"],
            "description": "信号类型：buying-买入信号，risk-风险信号，stall-停滞信号，commitment-承诺信号"
          },
          "content": {"type": "string", "description": "信号内容（引用客户原话）"},
          "timestamp": {"type": "string", "description": "信号出现的时间点"},
          "confidence": {
            "type": "string",
            "enum": ["high", "medium", "low"],
            "description": "置信度"
          }
        }
      }
    },
    "follow_up": {
      "type": "object",
      "required": ["next_action", "suggested_time", "talking_points"],
      "properties": {
        "next_action": {
          "type": "string",
          "enum": ["call", "send_materials", "demo", "proposal", "contract"],
          "description": "下一步行动"
        },
        "suggested_time": {"type": "string", "description": "建议跟进时间"},
        "talking_points": {
          "type": "array",
          "items": {"type": "string"},
          "description": "建议话术要点"
        },
        "materials_needed": {
          "type": "array",
          "items": {"type": "string"},
          "description": "需要准备的材料"
        }
      }
    },
    "deal_probability": {
      "type": "object",
      "required": ["level", "positive_factors", "negative_factors"],
      "properties": {
        "level": {
          "type": "string",
          "enum": ["high", "medium", "low"],
          "description": "成交概率"
        },
        "positive_factors": {
          "type": "array",
          "items": {"type": "string"},
          "description": "利好因素"
        },
        "negative_factors": {
          "type": "array",
          "items": {"type": "string"},
          "description": "不利因素"
        },
        "estimated_cycle": {"type": "string", "description": "预计成交周期"}
      }
    },
    "stage_suggestion": {
      "type": "object",
      "properties": {
        "current_stage": {
          "type": "string",
          "enum": ["first_contact", "discovery", "demo", "proposal", "negotiation"],
          "description": "当前阶段"
        },
        "suggested_stage": {
          "type": "string",
          "enum": ["first_contact", "discovery", "demo", "proposal", "negotiation", "won", "lost", "dormant"],
          "description": "建议阶段"
        },
        "confidence": {
          "type": "string",
          "enum": ["high", "medium", "low"],
          "description": "判断置信度"
        },
        "reason": {"type": "string", "description": "判断依据"}
      }
    },
    "key_quotes": {
      "type": "object",
      "properties": {
        "pain_points": {"type": "array", "items": {"type": "string"}, "description": "客户痛点原话"},
        "positive_signals": {"type": "array", "items": {"type": "string"}, "description": "客户积极信号"},
        "objections": {"type": "array", "items": {"type": "string"}, "description": "客户顾虑原话"},
        "effective_phrases": {"type": "array", "items": {"type": "string"}, "description": "销售人员的有效话术"}
      }
    },
    "conversation_summary": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "timerange": {"type": "string", "description": "时间段"},
          "topic": {"type": "string", "description": "话题"},
          "summary": {"type": "string", "description": "该段摘要"},
          "closer_dimension": {"type": "string", "description": "关联的CLOSER维度"},
          "score": {"type": "number", "description": "该段的评分"}
        }
      }
    }
  }
}',
    'v1',
    true,
    1,
    1,
    0,
    NOW(),
    NOW()
) ON CONFLICT (code) DO UPDATE SET
    name = EXCLUDED.name,
    description = EXCLUDED.description,
    category = EXCLUDED.category,
    system_prompt = EXCLUDED.system_prompt,
    user_prompt_template = EXCLUDED.user_prompt_template,
    output_schema = EXCLUDED.output_schema,
    version = EXCLUDED.version,
    updated_by = EXCLUDED.updated_by,
    updated_at = NOW();
