import { backup, DatabaseSync } from 'node:sqlite';
import { chmod, mkdir, readFile, readdir, realpath, rename, rm, stat, writeFile } from 'node:fs/promises';
import { basename, dirname, isAbsolute, join, relative, resolve, sep } from 'node:path';
import { randomBytes } from 'node:crypto';
import { parseArgs } from 'node:util';
import { pathToFileURL } from 'node:url';
import { loadConfig, loadFeishuConfig, type Config, type FeishuConfig } from './config.js';
import { QueryError } from './output.js';
import { lockDataDir } from './store.js';
import { connectQueryTools, safeFailure } from './tools.js';

async function libraryIdentity(config: Config) {
  const queries = await connectQueryTools(config);
  try {
    const result = await queries.call('list_libraries', {});
    if (!result.ok) throw new QueryError(result.error.code, result.error.message);
    const libraries = (result.data as { libraries: { id: string; directory: string }[] }).libraries;
    return JSON.stringify(libraries.map(({ id, directory }) => ({ id, directory })).sort((a, b) => a.id.localeCompare(b.id)));
  } finally { await queries.close(); }
}

// Source stays untouched except for the same directory lock used by the service.
export async function migrateState(options: {
  source: string; destination: string; runtimeRoot: string; sourceEndpoint: string;
  targetCheckEndpoint?: string;
  config: Config; scope: Pick<FeishuConfig, 'appId' | 'tenantKey' | 'chatId'>;
}) {
  const source = await realpath(options.source);
  const parent = await realpath(dirname(resolve(options.destination)));
  const destination = join(parent, basename(options.destination));
  if (!isAbsolute(options.runtimeRoot) || destination === source || !relative(source, destination).startsWith(`..${sep}`)) {
    throw new QueryError('invalid_destination', '迁移目标必须是源目录之外的新目录，运行目录必须为绝对路径');
  }
  try { await stat(destination); throw new QueryError('destination_exists', '目标目录已存在，迁移不会覆盖'); }
  catch (error) { if ((error as NodeJS.ErrnoException).code !== 'ENOENT') throw error; }
  const lock = lockDataDir(source);
  let input: DatabaseSync;
  try { input = new DatabaseSync(join(source, 'feishu.sqlite'), { readOnly: true }); }
  catch (error) { lock.close(); throw error; }
  const temporary = `${destination}.migrating-${randomBytes(6).toString('hex')}`;
  const scope = JSON.stringify({ appId: options.scope.appId, tenantKey: options.scope.tenantKey, chatId: options.scope.chatId });
  try {
    const owner = input.prepare('SELECT pid,token,scope FROM runtime WHERE id=1').get();
    if (owner?.scope !== scope) throw new QueryError('scope_mismatch', '源状态的租户、应用或群与目标配置不一致');
    if (owner.pid && !String(owner.token).startsWith('sqlite:')) {
      let running = true;
      try { process.kill(Number(owner.pid), 0); }
      catch (error) { if ((error as NodeJS.ErrnoException).code === 'ESRCH') running = false; }
      if (running) throw new QueryError('source_running', '请先停止源进程，再迁移状态');
    }
    const sourceConfig = { ...options.config, apiURL: options.sourceEndpoint.replace(/\/$/, '') };
    const targetCheck = options.targetCheckEndpoint ? { ...options.config, apiURL: options.targetCheckEndpoint.replace(/\/$/, '') } : options.config;
    if (await libraryIdentity(sourceConfig) !== await libraryIdentity(targetCheck)) {
      throw new QueryError('library_mismatch', '来源与目标库身份不一致');
    }
    await mkdir(temporary, { mode: 0o700 });
    await backup(input, join(temporary, 'feishu.sqlite'));
    const output = new DatabaseSync(join(temporary, 'feishu.sqlite'));
    let migrated = 0;
    try {
      output.prepare('UPDATE runtime SET pid=0,token=? WHERE id=1').run('');
      const tables = output.prepare("SELECT name FROM sqlite_master WHERE type='table' AND name='shelf_proposals'").all();
      if (tables.length) {
        for (const row of output.prepare('SELECT id,data FROM shelf_proposals').all()) {
          const proposal = JSON.parse(String(row.data));
          if (proposal.status === 'pending') proposal.expires = 0;
          output.prepare('UPDATE shelf_proposals SET data=? WHERE id=?').run(JSON.stringify(proposal), row.id);
        }
      }
      await mkdir(join(temporary, 'sessions'), { mode: 0o700 });
      for (const file of await readdir(join(source, 'sessions'))) {
        if (!file.endsWith('.jsonl')) continue;
        const path = join(source, 'sessions', file);
        if ((await realpath(path)) !== path) throw new QueryError('invalid_session', '会话文件不能指向目录外部');
        const lines = (await readFile(path, 'utf8')).split('\n');
        let context = false;
        const rewritten = lines.map(line => {
          if (!line.trim()) return line;
          const entry = JSON.parse(line);
          if (entry.type === 'session') {
            entry.cwd = join(options.runtimeRoot, 'workspace');
            if (entry.parentSession) {
              const name = basename(entry.parentSession);
              if (!name.endsWith('.jsonl')) throw new QueryError('invalid_parent', '父会话路径无效');
              entry.parentSession = join(options.runtimeRoot, 'sessions', name);
            }
            return JSON.stringify(entry);
          }
          if (entry.type === 'custom' && entry.customType === 'reliquary_query_context') {
            if (entry.data.version !== 1 || entry.data.endpoint !== sourceConfig.apiURL ||
                JSON.stringify(entry.data.libraries) !== JSON.stringify([...options.config.libraries].sort())) {
              throw new QueryError('session_scope_changed', '会话来源或目标库与迁移参数不一致');
            }
            entry.data.endpoint = options.config.apiURL;
            context = true;
            return JSON.stringify(entry);
          }
          return line;
        });
        if (!context) throw new QueryError('missing_context', '会话缺少来源范围');
        await writeFile(join(temporary, 'sessions', file), rewritten.join('\n'), { mode: 0o600 });
        migrated++;
      }
      for (const row of output.prepare('SELECT session_file,parent_file FROM conversations').all()) {
        for (const file of [row.session_file, row.parent_file].filter(Boolean)) {
          if (basename(String(file)) !== file) throw new QueryError('invalid_session', '数据库会话路径无效');
          await stat(join(temporary, 'sessions', String(file)));
        }
      }
      await mkdir(join(temporary, 'workspace'), { mode: 0o700 });
      // Pi-owned OAuth credentials are state; external Codex credentials remain externally mounted.
      try { await writeFile(join(temporary, 'auth.json'), await readFile(join(source, 'auth.json')), { mode: 0o600 }); }
      catch (error) { if ((error as NodeJS.ErrnoException).code !== 'ENOENT') throw error; }
    } finally { output.close(); }
    await chmod(join(temporary, 'feishu.sqlite'), 0o600);
    await rename(temporary, destination);
    return { sessions: migrated, destination, runtimeRoot: options.runtimeRoot };
  } catch (error) {
    await rm(temporary, { recursive: true, force: true });
    throw error;
  } finally { input.close(); lock.close(); }
}

if (process.argv[1] && import.meta.url === pathToFileURL(resolve(process.argv[1])).href) {
  try {
    const { values } = parseArgs({ options: { from: { type: 'string' }, 'from-endpoint': { type: 'string' },
      'target-check-endpoint': { type: 'string' }, 'runtime-root': { type: 'string', default: '/data' } } });
    if (!values.from || !values['from-endpoint']) throw new QueryError('missing_arguments', '需要 --from 与 --from-endpoint，目标目录由 AGENT_DATA_DIR 指定');
    const config = loadConfig();
    console.log(JSON.stringify(await migrateState({ source: values.from, destination: config.dataDir,
      runtimeRoot: values['runtime-root']!, sourceEndpoint: values['from-endpoint'],
      targetCheckEndpoint: values['target-check-endpoint'], config, scope: loadFeishuConfig() })));
  } catch (error) { console.error(JSON.stringify(safeFailure(error))); process.exitCode = 1; }
}
