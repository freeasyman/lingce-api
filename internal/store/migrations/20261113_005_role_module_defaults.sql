-- 收费/挂号/药房按「模块权限」授权（ABC 模型）：补齐默认角色模块，避免只有管理员能操作。
INSERT INTO his_role_modules (tenant_id, role, module) VALUES
  (0, 'assistant', 'register'), (0, 'assistant', 'billing'),
  (0, 'other',     'register'), (0, 'other',     'billing'), (0, 'other', 'pharmacy'),
  (0, 'nurse',     'pharmacy')
ON CONFLICT (tenant_id, role, module) DO NOTHING;
