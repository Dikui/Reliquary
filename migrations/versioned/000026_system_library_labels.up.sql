UPDATE libraries AS library
SET name = labels.new_name
FROM (VALUES
    ('institutional', '机构发布', '官方信息'),
    ('opinions', '人物观点', '媒体信息'),
    ('references', '参考文献', '参考文档'),
    ('mine', '我的资料', '个人产出'),
    ('ai-reports', 'AI 报告', 'AI产出')
) AS labels(system_role, old_name, new_name)
WHERE library.system_role = labels.system_role AND library.name = labels.old_name;
