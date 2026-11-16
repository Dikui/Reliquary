import type { Config } from './config.js';
import { QueryAgent } from './pi.js';
import { safeFailure, type QueryTools } from './tools.js';
import { DeliveryError, type FeishuTransport } from './feishu.js';
import { Store, type Job } from './store.js';
import { ShelfEditor, type ShelfProposal } from './shelf.js';
import { attachmentTool } from './attachment.js';

export class Worker {
  private modelWork: Promise<void> | undefined;
  private deliveryWork: Promise<void> | undefined;
  private stopping = false;
  private active?: QueryAgent;
  private lastReconcile = 0;
  lastError: string | null = null;

  constructor(private config: Config, private queries: QueryTools, readonly store: Store,
    private transport: Pick<FeishuTransport, 'reply' | 'reconcile'> & Partial<Pick<FeishuTransport, 'download'>>,
    private log: (event: string, details?: object) => void,
    private shelf?: ShelfEditor) {}

  private async proposalOutput(job: Job, proposal: ShelfProposal, message?: string) {
    const text = message ?? this.shelf!.render(proposal);
    const conversation = this.store.conversation(proposal.conversationId);
    const file = job.session_file ?? conversation?.session_file;
    const answer = file ? await QueryAgent.recordHost(this.config, file, String(job.id), text) : { text };
    return { ...answer, proposalId: proposal.id };
  }

  private async recoverJob(job: Job, reason: string) {
    const proposal = this.shelf && this.store.proposalForJob(job.id);
    if (proposal) {
      if (['preparing', 'unknown', 'committing'].includes(proposal.status)) await this.shelf!.recover(proposal);
      this.store.finish(job, await this.proposalOutput(job, proposal));
      return;
    }
    const recovered = await QueryAgent.recover(this.config, String(job.id), job.session_file, job.base_leaf);
    if (job.session_file) this.store.restoreConversation(job, recovered.sessionFile);
    if (recovered.answer) this.store.finish(job, recovered.answer);
    else this.store.finish(job, { text: `本轮查询已中断。${reason}。请回复本消息并 @机器人 重新提问；已完成的上下文已保留。` }, 'interrupted');
    this.log('job_recovered', { jobId: job.id, completed: Boolean(recovered.answer) });
  }

  async recover() {
    await this.reconcileProposals();
    for (const job of this.store.runningJobs()) await this.recoverJob(job, '进程重启前未完成');
  }

  private async reconcileProposals() {
    if (!this.shelf || Date.now() - this.lastReconcile < 10000) return;
    this.lastReconcile = Date.now();
    for (const proposal of this.store.unsettledProposals()) {
      await this.shelf.recover(proposal);
      if (['pending', 'confirmed'].includes(proposal.status)) {
        const job = this.store.db.prepare('SELECT * FROM jobs WHERE id=?').get(proposal.jobId) as Job;
        if (job.status !== 'running') {
          const output = await this.proposalOutput(job, proposal, `服务端结果已核对。\n${this.shelf.render(proposal)}`);
          this.store.enqueueUpdate(job.id, output);
        }
      }
    }
  }

  async runNextJob(): Promise<boolean> {
    await this.reconcileProposals();
    const job = this.store.nextJob();
    if (!job) return false;
    if (job.rejection) {
      this.store.finish(job, { text: job.rejection }, 'rejected');
      return true;
    }
    let started = false;
    try {
      // A reply event may arrive before the outgoing API response has been stored.
      if (job.parent_id) await this.deliveryWork;
      if (job.parent_id && !this.store.hasReply(job.parent_id)) {
        for (const row of this.store.unconfirmedOutgoing()) {
          const found = await this.transport.reconcile(row);
          if (found) this.store.sent(row, found.messageId);
          if (found?.messageId === job.parent_id) break;
        }
      }
      const conversation = this.store.route(job);
      if (this.shelf) {
        const command = await this.shelf.command(job);
        if (command) {
          const output = command.proposal ? await this.proposalOutput(job, command.proposal, command.text) : { text: command.text };
          this.store.finish(job, output);
          return true;
        }
      }
      const additionalTools = [];
      if (this.shelf) {
        additionalTools.push(this.shelf.entryDefinition(job));
        if (this.transport.download) additionalTools.push(attachmentTool(this.config, this.queries, this.store, job,
          { download: this.transport.download.bind(this.transport) }, this.shelf));
      }
      this.active = await QueryAgent.open(this.config, this.queries, {
        sessionFile: conversation.session_file ?? conversation.parent_file ?? undefined,
        answerEntryId: conversation.session_file ? undefined : conversation.branch_entry ?? undefined,
        proposalTool: this.shelf?.definition(job), shelfContext: this.shelf?.context(job),
        additionalTools,
      });
      const checkpoint = this.active.checkpoint();
      this.store.started(job, checkpoint.sessionFile, checkpoint.baseLeaf);
      started = true;
      const answer = await this.active.ask(job.text, String(job.id));
      const proposal = this.shelf && this.store.proposalForJob(job.id);
      const pending = this.shelf && this.store.activeProposal(job.conversation_id!, job.sender_id);
      const output = proposal ? await this.proposalOutput(job, proposal) : pending
        ? await this.proposalOutput(job, pending, `${answer.text}\n\n${this.shelf!.render(pending)}`) : answer;
      this.store.finish(job, output);
      this.log('job_succeeded', { jobId: job.id, answerEntryId: answer.answerEntryId });
      this.lastError = null;
    } catch (error) {
      const failure = safeFailure(error).error;
      this.lastError = failure.code;
      if (started || this.shelf && this.store.proposalForJob(job.id)) await this.recoverJob(job, failure.message);
      else this.store.finish(job, { text: failure.message }, 'failed', failure.code);
      this.log('job_failed', { jobId: job.id, code: failure.code });
    } finally {
      await this.active?.close();
      this.active = undefined;
    }
    return true;
  }

  flushOutbox(): Promise<void> {
    if (!this.deliveryWork) {
      this.deliveryWork = this.sendOutgoing().finally(() => { this.deliveryWork = undefined; });
    }
    return this.deliveryWork;
  }

  private async sendOutgoing() {
    for (const row of this.store.dueOutgoing()) {
      try {
        if (row.status !== 'pending') {
          const found = await this.transport.reconcile(row);
          if (found) { this.store.sent(row, found.messageId); continue; }
          // Feishu UUID deduplication expires after one hour. Keep ambiguous old sends for review.
          if (!row.first_attempt || Date.now() - row.first_attempt >= 55 * 60 * 1000) {
            this.store.deliveryProblem(row, 'unknown', 'delivery_not_confirmed');
            continue;
          }
        }
        this.store.sending(row);
        const result = await this.transport.reply(row);
        this.store.sent(row, result.messageId);
        this.log('reply_sent', { jobId: row.job_id, part: row.part, messageId: result.messageId });
      } catch (error) {
        const failure = error instanceof DeliveryError ? error : new DeliveryError('unknown', 'delivery_transport_interrupted');
        const status = row.status === 'pending' ? (failure.kind === 'retry' ? 'pending' : failure.kind) : 'unknown';
        this.store.deliveryProblem(row, status, failure.code);
        this.log('reply_failed', { jobId: row.job_id, status: failure.kind, code: failure.code });
      }
    }
  }

  wake() {
    if (this.stopping || this.modelWork) return;
    this.modelWork = (async () => {
      while (!this.stopping && await this.runNextJob()) this.deliver();
    })().catch(() => { this.lastError = 'worker_error'; this.log('worker_error'); }).finally(() => { this.modelWork = undefined; });
  }

  deliver() {
    if (this.stopping || this.deliveryWork) return;
    void this.flushOutbox().catch(() => this.log('delivery_error'));
  }

  async close() {
    this.stopping = true;
    await this.active?.session.abort();
    await Promise.all([this.modelWork, this.deliveryWork]);
  }
}
