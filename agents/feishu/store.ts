import { DatabaseSync } from 'node:sqlite';
import { chmodSync, mkdirSync } from 'node:fs';
import { join } from 'node:path';
import { randomUUID } from 'node:crypto';
import type { FeishuConfig } from './config.js';
import { QueryError } from './output.js';
import type { ShelfProposal } from './shelf.js';

export type IncomingMessage = {
  messageId: string; parentId: string | null; eventId: string; senderId: string;
  text: string; branch: boolean; rejection?: string;
};
export type Job = {
  id: number; message_id: string; parent_id: string | null; text: string; branch: number; sender_id: string;
  rejection: string | null; conversation_id: string | null; status: string;
  session_file: string | null; base_leaf: string | null;
};
export type Conversation = {
  id: string; session_file: string | null; parent_file: string | null; branch_entry: string | null;
};
export type Outgoing = {
  id: number; job_id: number; part: number; uuid: string; message_id: string | null;
  reply_to: string; content: string; status: string; first_attempt: number | null;
  attempts: number; next_attempt: number; answer_entry: string | null; session_file: string | null;
  proposal_id?: string | null;
};
export type CompletedOutput = { text: string; answerEntryId?: string; sessionFile?: string; proposalId?: string };

// A separate SQLite transaction owns the directory across process/PID namespaces.
export function lockDataDir(dataDir: string): DatabaseSync {
  mkdirSync(dataDir, { recursive: true, mode: 0o700 });
  const path = join(dataDir, 'runtime-lock.sqlite');
  const lock = new DatabaseSync(path);
  chmodSync(path, 0o600);
  try {
    lock.exec('PRAGMA busy_timeout=0; BEGIN EXCLUSIVE; CREATE TABLE IF NOT EXISTS ownership (id INTEGER PRIMARY KEY)');
    return lock;
  } catch {
    lock.close();
    throw new QueryError('already_running', '该数据目录已有运行中的飞书进程或维护操作');
  }
}

export function splitReply(text: string): string[] {
  const parts: string[] = [];
  let current = '';
  let size = 0;
  // Leave ample room for JSON escaping and the segment label in Feishu's payload limit.
  for (const char of text) {
    const bytes = Buffer.byteLength(char);
    if (size + bytes > 12000) { parts.push(current); current = ''; size = 0; }
    current += char;
    size += bytes;
  }
  if (current) parts.push(current);
  return parts.map((part, index) => parts.length > 1 ? `(${index + 1}/${parts.length})\n${part}` : part);
}

export class Store {
  readonly db: DatabaseSync;
  private token = `sqlite:${randomUUID()}`;
  private closed = false;
  private lock: DatabaseSync;

  constructor(dataDir: string, readonly scope: Pick<FeishuConfig, 'appId' | 'tenantKey' | 'chatId'>) {
    this.lock = lockDataDir(dataDir);
    const path = join(dataDir, 'feishu.sqlite');
    try { this.db = new DatabaseSync(path); }
    catch (error) { this.lock.close(); throw error; }
    try {
      chmodSync(path, 0o600);
      this.db.exec(`PRAGMA busy_timeout=200; PRAGMA journal_mode=WAL; PRAGMA synchronous=FULL; PRAGMA foreign_keys=ON;
        CREATE TABLE IF NOT EXISTS runtime (id INTEGER PRIMARY KEY CHECK(id=1), pid INTEGER NOT NULL, token TEXT NOT NULL, scope TEXT NOT NULL);
        CREATE TABLE IF NOT EXISTS conversations (
          id TEXT PRIMARY KEY, session_file TEXT, parent_file TEXT, branch_entry TEXT
        );
        CREATE TABLE IF NOT EXISTS jobs (
          id INTEGER PRIMARY KEY, tenant TEXT NOT NULL, app TEXT NOT NULL, chat TEXT NOT NULL,
          message_id TEXT NOT NULL, parent_id TEXT, event_id TEXT NOT NULL, sender_id TEXT NOT NULL,
          text TEXT NOT NULL, branch INTEGER NOT NULL, rejection TEXT, conversation_id TEXT REFERENCES conversations(id),
          status TEXT NOT NULL DEFAULT 'queued', session_file TEXT, base_leaf TEXT, error TEXT,
          UNIQUE(tenant, app, message_id)
        );
        CREATE TABLE IF NOT EXISTS outbox (
          id INTEGER PRIMARY KEY, job_id INTEGER NOT NULL REFERENCES jobs(id), part INTEGER NOT NULL,
          uuid TEXT NOT NULL UNIQUE, reply_to TEXT NOT NULL, content TEXT NOT NULL,
          status TEXT NOT NULL DEFAULT 'pending', message_id TEXT UNIQUE,
          first_attempt INTEGER, attempts INTEGER NOT NULL DEFAULT 0, next_attempt INTEGER NOT NULL DEFAULT 0,
          answer_entry TEXT, session_file TEXT, error TEXT, UNIQUE(job_id, part)
        );
        CREATE TABLE IF NOT EXISTS shelf_proposals (
          id TEXT PRIMARY KEY, conversation_id TEXT NOT NULL, owner_id TEXT NOT NULL,
          job_id INTEGER NOT NULL, status TEXT NOT NULL, data TEXT NOT NULL
        );`);
      const columns = this.db.prepare('PRAGMA table_info(outbox)').all();
      if (!columns.some(column => column.name === 'proposal_id')) this.db.exec('ALTER TABLE outbox ADD COLUMN proposal_id TEXT');
      if (!columns.some(column => column.name === 'proposal_token')) this.db.exec('ALTER TABLE outbox ADD COLUMN proposal_token TEXT');
      this.transaction(() => {
        const owner = this.db.prepare('SELECT * FROM runtime WHERE id=1').get() as { pid: number; scope: string; token: string } | undefined;
        const key = JSON.stringify({ appId: scope.appId, tenantKey: scope.tenantKey, chatId: scope.chatId });
        if (owner && owner.scope !== key) throw new QueryError('store_scope_changed', '数据目录已绑定其他飞书租户、应用或群');
        if (owner?.pid && !owner.token.startsWith('sqlite:')) {
          let alive = true;
          try { process.kill(owner.pid, 0); }
          catch (error) { if ((error as NodeJS.ErrnoException).code === 'ESRCH') alive = false; }
          if (alive) throw new QueryError('already_running', '请先停止使用此目录的旧版进程，或执行离线迁移');
        }
        this.db.prepare('INSERT OR REPLACE INTO runtime VALUES (1, ?, ?, ?)').run(process.pid, this.token, key);
      });
    } catch (error) { this.db.close(); this.lock.close(); throw error; }
  }

  private transaction<T>(action: () => T): T {
    this.db.exec('BEGIN IMMEDIATE');
    try { const result = action(); this.db.exec('COMMIT'); return result; }
    catch (error) { this.db.exec('ROLLBACK'); throw error; }
  }

  accept(message: IncomingMessage): boolean {
    const result = this.db.prepare(`INSERT OR IGNORE INTO jobs
      (tenant, app, chat, message_id, parent_id, event_id, sender_id, text, branch, rejection)
      VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`).run(this.scope.tenantKey, this.scope.appId, this.scope.chatId,
        message.messageId, message.parentId, message.eventId, message.senderId, message.text,
        Number(message.branch), message.rejection ?? null);
    return result.changes === 1;
  }

  nextJob(): Job | undefined {
    return this.db.prepare("SELECT * FROM jobs WHERE status='queued' ORDER BY id LIMIT 1").get() as Job | undefined;
  }

  runningJobs(): Job[] {
    return this.db.prepare("SELECT * FROM jobs WHERE status='running' ORDER BY id").all() as Job[];
  }

  route(job: Job): Conversation {
    return this.transaction(() => {
      let conversation: Conversation | undefined;
      if (job.parent_id) {
        const parent = this.db.prepare(`SELECT o.*, j.conversation_id FROM outbox o JOIN jobs j ON j.id=o.job_id
          WHERE o.message_id=? AND o.status='sent'`).get(job.parent_id) as (Outgoing & { conversation_id: string | null }) | undefined;
        if (!parent?.conversation_id) throw new QueryError('unknown_reply', '这条回复尚未关联到本机会话，请重新 @机器人 提问');
        if (job.branch) {
          if (!parent.answer_entry || !parent.session_file) throw new QueryError('invalid_branch', '请回复一条已完成的答案，再使用“分支：问题”');
          conversation = { id: randomUUID(), session_file: null, parent_file: parent.session_file, branch_entry: parent.answer_entry };
        } else conversation = this.conversation(parent.conversation_id);
      } else {
        if (job.branch) throw new QueryError('invalid_branch', '请回复要派生的答案，再使用“分支：问题”');
        conversation = { id: randomUUID(), session_file: null, parent_file: null, branch_entry: null };
      }
      if (!conversation) throw new QueryError('unknown_session', '会话映射缺失，请重新 @机器人 提问');
      this.db.prepare('INSERT OR IGNORE INTO conversations VALUES (?, ?, ?, ?)')
        .run(conversation.id, conversation.session_file, conversation.parent_file, conversation.branch_entry);
      this.db.prepare("UPDATE jobs SET conversation_id=?, status='running' WHERE id=? AND status='queued'")
        .run(conversation.id, job.id);
      job.conversation_id = conversation.id;
      return conversation;
    });
  }

  conversation(id: string): Conversation | undefined {
    return this.db.prepare('SELECT * FROM conversations WHERE id=?').get(id) as Conversation | undefined;
  }

  hasReply(messageId: string): boolean {
    return Boolean(this.db.prepare("SELECT 1 FROM outbox WHERE message_id=? AND status='sent'").get(messageId));
  }

  unconfirmedOutgoing(): Outgoing[] {
    return this.db.prepare("SELECT * FROM outbox WHERE status IN ('sending','unknown') ORDER BY id DESC").all() as Outgoing[];
  }

  started(job: Job, sessionFile: string, baseLeaf: string | null) {
    this.transaction(() => {
      this.db.prepare('UPDATE conversations SET session_file=? WHERE id=?').run(sessionFile, job.conversation_id);
      this.db.prepare('UPDATE jobs SET session_file=?, base_leaf=? WHERE id=?').run(sessionFile, baseLeaf, job.id);
    });
    job.session_file = sessionFile;
    job.base_leaf = baseLeaf;
  }

  finish(job: Job, output: CompletedOutput, status = 'succeeded', error: string | null = null) {
    this.transaction(() => {
      this.db.prepare('UPDATE jobs SET status=?, error=? WHERE id=?').run(status, error, job.id);
      const parts = splitReply(output.text);
      parts.forEach((content, part) => {
        this.db.prepare(`INSERT OR IGNORE INTO outbox
          (job_id, part, uuid, reply_to, content, answer_entry, session_file, proposal_id, proposal_token) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`)
          .run(job.id, part, randomUUID(), job.message_id, content, output.answerEntryId ?? null, output.sessionFile ?? null,
            part === parts.length - 1 ? output.proposalId ?? null : null,
            output.proposalId ? this.proposal(output.proposalId)?.token ?? null : null);
      });
    });
  }

  saveProposal(proposal: ShelfProposal) {
    this.db.prepare(`INSERT INTO shelf_proposals VALUES (?, ?, ?, ?, ?, ?)
      ON CONFLICT(id) DO UPDATE SET job_id=excluded.job_id, status=excluded.status, data=excluded.data`)
      .run(proposal.id, proposal.conversationId, proposal.ownerId, proposal.jobId, proposal.status, JSON.stringify(proposal));
  }

  proposal(id: string): ShelfProposal | undefined {
    const row = this.db.prepare('SELECT data FROM shelf_proposals WHERE id=?').get(id);
    return row ? JSON.parse(String(row.data)) : undefined;
  }

  activeProposal(conversationId: string, ownerId: string): ShelfProposal | undefined {
    const row = this.db.prepare(`SELECT data FROM shelf_proposals WHERE conversation_id=? AND owner_id=?
      AND status IN ('preparing','pending','committing','unknown') ORDER BY rowid DESC LIMIT 1`).get(conversationId, ownerId);
    return row ? JSON.parse(String(row.data)) : undefined;
  }

  proposalForJob(jobId: number): ShelfProposal | undefined {
    const row = this.db.prepare('SELECT data FROM shelf_proposals WHERE job_id=? ORDER BY rowid DESC LIMIT 1').get(jobId);
    return row ? JSON.parse(String(row.data)) : undefined;
  }

  previewProposal(messageId: string): ShelfProposal | undefined {
    const row = this.db.prepare("SELECT proposal_id,proposal_token FROM outbox WHERE message_id=? AND status='sent'").get(messageId);
    const proposal = row?.proposal_id ? this.proposal(String(row.proposal_id)) : undefined;
    return proposal?.token === row?.proposal_token ? proposal : undefined;
  }

  unsettledProposals(): ShelfProposal[] {
    return this.db.prepare("SELECT data FROM shelf_proposals WHERE status IN ('preparing','unknown','committing')")
      .all().map(row => JSON.parse(String(row.data)));
  }

  health() {
    this.db.prepare('SELECT 1').get();
    return { queued: Number(this.db.prepare("SELECT count(*) AS n FROM jobs WHERE status='queued'").get()!.n),
      pendingProposals: Number(this.db.prepare("SELECT count(*) AS n FROM shelf_proposals WHERE status='pending'").get()!.n) };
  }

  enqueueUpdate(jobId: number, output: CompletedOutput) {
    this.transaction(() => {
      const job = this.db.prepare('SELECT message_id FROM jobs WHERE id=?').get(jobId)!;
      const next = Number(this.db.prepare('SELECT COALESCE(MAX(part),-1)+1 AS part FROM outbox WHERE job_id=?').get(jobId)!.part);
      const parts = splitReply(output.text);
      parts.forEach((content, index) => this.db.prepare(`INSERT INTO outbox
        (job_id,part,uuid,reply_to,content,answer_entry,session_file,proposal_id,proposal_token) VALUES (?,?,?,?,?,?,?,?,?)`)
        .run(jobId, next + index, randomUUID(), String(job.message_id), content, output.answerEntryId ?? null,
          output.sessionFile ?? null, index === parts.length - 1 ? output.proposalId ?? null : null,
          output.proposalId ? this.proposal(output.proposalId)?.token ?? null : null));
    });
  }

  restoreConversation(job: Job, sessionFile: string | null) {
    this.db.prepare('UPDATE conversations SET session_file=?, parent_file=NULL, branch_entry=NULL WHERE id=?')
      .run(sessionFile, job.conversation_id);
  }

  dueOutgoing(): Outgoing[] {
    return this.db.prepare(`SELECT o.* FROM outbox o WHERE status IN ('pending','sending','unknown') AND next_attempt<=?
      AND NOT EXISTS (SELECT 1 FROM outbox previous WHERE previous.job_id=o.job_id AND previous.part<o.part AND previous.status!='sent')
      ORDER BY id`).all(Date.now()) as Outgoing[];
  }

  sending(row: Outgoing) {
    this.db.prepare("UPDATE outbox SET status='sending', first_attempt=COALESCE(first_attempt,?), attempts=attempts+1 WHERE id=?")
      .run(Date.now(), row.id);
  }

  sent(row: Outgoing, messageId: string) {
    this.db.prepare("UPDATE outbox SET status='sent', message_id=?, error=NULL WHERE id=?").run(messageId, row.id);
  }

  deliveryProblem(row: Outgoing, status: 'unknown' | 'pending' | 'failed', error: string) {
    const delay = Math.min(60000, 1000 * 2 ** Math.min(row.attempts, 6));
    this.db.prepare('UPDATE outbox SET status=?, error=?, next_attempt=? WHERE id=?').run(status, error, Date.now() + delay, row.id);
  }

  close() {
    if (this.closed) return;
    this.closed = true;
    try { this.db.prepare('UPDATE runtime SET pid=0 WHERE id=1 AND token=?').run(this.token); }
    finally { this.db.close(); this.lock.close(); }
  }
}
