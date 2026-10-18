UPDATE libraries AS library
SET name = labels.new_name
FROM (VALUES
    ('institutional', '官方信息', '机构发布'),
    ('opinions', '媒体信息', '人物观点'),
    ('references', '参考文档', '参考文献'),
    ('mine', '个人产出', '我的资料'),
    ('ai-reports', 'AI产出', 'AI 报告')
) AS labels(system_role, old_name, new_name)
WHERE library.system_role = labels.system_role AND library.name = labels.old_name;
