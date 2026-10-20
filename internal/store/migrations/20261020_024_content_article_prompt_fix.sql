-- Article prompt should be specialty-neutral and cite verifiable guidance,
-- not force a TCM reference into an ophthalmology article.
UPDATE content_prompt_templates
SET prompt_template = replace(
      replace(prompt_template,
        '3. 适当引用中医经典或现代医学研究',
        '3. 引用可核验的临床指南、机构资料或医生确认过的事实，不编造研究'),
      '4. 结尾要有总结和行动建议',
      '4. 结尾要有总结和行动建议，并写清不适用与风险边界'),
    updated_at = NOW()
WHERE code = 'content_article';
