package types

import (
	"go/scanner"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

type namingException struct {
	Reason string
	Tokens []string
}

// Each exception allows specific literal tokens in a specific file. Go
// identifiers are checked separately and have no exceptions.
var namingExceptions = map[string]namingException{
	"cmd/server/maintenance_sources_pg_test.go":                            {Reason: "冻结基线恢复的真实离线 CLI 顺序使用旧归属列夹具", Tokens: []string{"tenant_id"}},
	"internal/maintenance/collection_migration_pg_test.go":                 {Reason: "冻结基线备份恢复到当前版本的真实旧表与归属列夹具", Tokens: []string{"tenants", "tenant_id"}},
	"internal/application/service/entry_reprocess_missing_pg_test.go":      {Reason: "验证旧采集任务 payload 不再联网且保留旧文章状态", Tokens: []string{"tenant_id"}},
	"internal/testutil/pgtest/entry_migration_test.go":                     {Reason: "条目迁移夹具保留原队列归属键", Tokens: []string{"tenant_id"}},
	"internal/router/mcp_pg_test.go":                                       {Reason: "MCP 旧响应转换为测试内部 DTO", Tokens: []string{"tenant_id"}},
	"frontend/src/stores/auth.ts":                                          {Reason: "已持久化的浏览器会话存储键与旧用户缓存转换，HTTP 不接受旧字段", Tokens: []string{"reliquary_tenant", "tenant_id"}},
	"frontend/src/utils/requestAvailability.test.ts":                       {Reason: "旧浏览器会话升级后经过实际请求拦截器及 store 加载新字段", Tokens: []string{"reliquary_tenant", "tenant_id"}},
	"internal/application/service/topic_management_pg_test.go":             {Reason: "真实数据库断言主题操作日志保留内部旧键", Tokens: []string{"tenant_id"}},
	"internal/application/service/system_setting_retired_test.go":          {Reason: "验证旧持久化设置键继续隐藏，取值不改名", Tokens: []string{"tenant"}},
	"internal/application/service/operation_value.go":                      {Reason: "操作日志中已持久化的主题与关联行 JSON 键保持不变", Tokens: []string{"tenant_id"}},
	"internal/application/service/operation_value_test.go":                 {Reason: "验证操作日志内部旧键及不改写任意用户 JSON", Tokens: []string{"tenant_id"}},
	"internal/mcpserver/contract_test.go":                                  {Reason: "验证 MCP 响应字段兼容并保留不透明用户 JSON", Tokens: []string{"tenant_id"}},
	"cmd/server/maintenance_credentials.go":                                {Reason: "旧离线 CLI 参数继续接受并打印弃用警告", Tokens: []string{"tenant"}},
	"internal/application/service/entity_embedding_maintenance_pg_test.go": {Reason: "验证旧模型 YAML 归属键升级及原队列 payload 可读", Tokens: []string{"tenant_id"}},
	"internal/application/service/system_setting.go":                       {Reason: "删除已退休的持久设置键，升级时仍按旧键清理", Tokens: []string{"tenant"}},
	"internal/application/service/user.go":                                 {Reason: "现存 JWT 的 claim 保持可读，签发取值不变", Tokens: []string{"tenant_id"}},
	"internal/application/service/user_jwt_test.go":                        {Reason: "验证已有 JWT 的旧 claim 保持可读及刷新行为", Tokens: []string{"tenant_id"}},
	"internal/maintenance/format.go":                                       {Reason: "版本 1 副本和备份清单的表名与行外层列名转换，内部 JSON 不改", Tokens: []string{"tenant_api_keys", "tenant_id", "tenants"}},
	"internal/maintenance/format_pg_test.go":                               {Reason: "构造版本 1 副本并验证嵌套 JSON 与任务 payload 不改写", Tokens: []string{"tenant_id"}},
	"internal/maintenance/schema_compat.go":                                {Reason: "识别未升级数据库以在事务中供离线维护读取，结束还原原结构", Tokens: []string{"tenants"}},
	"internal/mcpserver/server.go":                                         {Reason: "MCP 工具响应保留对外契约字段", Tokens: []string{"tenant_id"}},
	"internal/middleware/auth.go":                                          {Reason: "原有可选 HTTP 请求头保留，保持认证检查行为", Tokens: []string{"tenant"}},
	"internal/queue/dead_letter_test.go":                                   {Reason: "验证旧任务归属字段诊断及死信 scope 取值不变", Tokens: []string{"tenant_id"}},
	"internal/queue/queue_pg_test.go":                                      {Reason: "真实队列夹具保留旧任务 payload 字段", Tokens: []string{"tenant_id"}},
	"internal/queue/worker.go":                                             {Reason: "旧任务 payload 的持久字段、双键读取或诊断探针，写入键不变", Tokens: []string{"tenant_id"}},
	"internal/router/space_naming_pg_test.go":                              {Reason: "真实 HTTP 否定旧响应字段并执行新旧 payload 任务", Tokens: []string{"tenant_id"}},
	"internal/router/task.go":                                              {Reason: "旧任务 payload 的持久字段、双键读取或诊断探针，写入键不变", Tokens: []string{"tenant_id"}},
	"internal/router/task_inspector.go":                                    {Reason: "旧任务 payload 的持久字段、双键读取或诊断探针，写入键不变", Tokens: []string{"tenant_id"}},
	"internal/router/task_inspector_runtime_test.go":                       {Reason: "验证旧任务 payload 在诊断界面仍能解析归属", Tokens: []string{"tenant_id"}},
	"internal/router/task_team_test.go":                                    {Reason: "验证后台中间件接受旧任务归属键并拒绝非法归属", Tokens: []string{"tenant_id"}},
	"internal/testutil/pgtest/maintenance_migration_test.go":               {Reason: "维护迁移版本 0 的原表名及行归属夹具", Tokens: []string{"tenant_id", "tenants"}},
	"internal/testutil/pgtest/space_migration_test.go":                     {Reason: "版本 1 原结构数据夹具及无旧 schema 对象的目录断言", Tokens: []string{"tenant", "tenant_api_keys", "tenant_id", "tenants"}},
	"internal/types/builtin_models_config.go":                              {Reason: "接受旧 YAML 配置键并打印弃用警告", Tokens: []string{"tenant_id"}},
	"internal/types/builtin_models_space_test.go":                          {Reason: "验证旧 YAML 配置键兼容、弃用警告及新键优先级", Tokens: []string{"tenant_id"}},
	"internal/types/embedding_rebuild.go":                                  {Reason: "旧任务 payload 的持久字段、双键读取或诊断探针，写入键不变", Tokens: []string{"tenant_id"}},
	"internal/types/entity_name_embedding_rebuild.go":                      {Reason: "旧任务 payload 的持久字段、双键读取或诊断探针，写入键不变", Tokens: []string{"tenant_id"}},
	"internal/types/space_payload.go":                                      {Reason: "旧任务 payload 的持久字段、双键读取或诊断探针，写入键不变", Tokens: []string{"tenant_id"}},
	"internal/types/space_payload_test.go":                                 {Reason: "验证八类旧任务 payload、双键兼容和冲突拒绝", Tokens: []string{"tenant_id"}},
	"internal/types/task.go":                                               {Reason: "旧任务 payload 的持久字段、双键读取或诊断探针，写入键不变", Tokens: []string{"tenant_id"}},
	"internal/types/task_dead_letter.go":                                   {Reason: "死信的已持久化 scope 取值不变", Tokens: []string{"tenant"}},
	"cmd/server/maintenance_legacy_pg_test.go":                             {Reason: "验证旧结构离线维护后原版本及 schema 保持不变", Tokens: []string{"tenant_id", "tenants"}},
	"frontend/src/i18n/locales/workspaceTerminology.test.ts":               {Reason: "负面断言界面文案不含已退出的旧领域术语", Tokens: []string{"tenants"}},
}

func TestSpaceNamingGuard(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	stem := "ten" + "ant"
	words := regexp.MustCompile(`[A-Za-z_][A-Za-z_0-9]*`)
	for _, directory := range []string{"internal", "cmd", "frontend/src"} {
		err := filepath.WalkDir(filepath.Join(root, directory), func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				if entry.Name() == "testdata" {
					return filepath.SkipDir
				}
				return nil
			}
			relative, _ := filepath.Rel(root, path)
			relative = filepath.ToSlash(relative)
			if relative == "internal/types/space_naming_guard_test.go" {
				return nil
			} // The literal whitelist itself.
			extension := filepath.Ext(path)
			if extension != ".go" && extension != ".ts" && extension != ".vue" {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if extension == ".go" {
				set := token.NewFileSet()
				file := set.AddFile(path, -1, len(data))
				var scan scanner.Scanner
				scan.Init(file, data, nil, 0)
				for {
					position, kind, literal := scan.Scan()
					if kind == token.EOF {
						break
					}
					if kind == token.IDENT && strings.Contains(strings.ToLower(literal), stem) {
						t.Errorf("%s: obsolete identifier %s", set.Position(position), literal)
					}
				}
			}
			exception := namingExceptions[relative]
			for index, line := range strings.Split(string(data), "\n") {
				for _, word := range words.FindAllString(line, -1) {
					if !strings.Contains(strings.ToLower(word), stem) {
						continue
					}
					allowed := false
					for _, literal := range exception.Tokens {
						if strings.EqualFold(word, literal) {
							allowed = true
							break
						}
					}
					if !allowed || exception.Reason == "" {
						t.Errorf("%s:%d: obsolete token %s has no documented exception", relative, index+1, word)
					}
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
