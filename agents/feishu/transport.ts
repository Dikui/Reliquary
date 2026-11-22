import type { IncomingMessage, Outgoing } from './store.js';

export type Delivery = { messageId: string };

export class DeliveryError extends Error {
  constructor(public kind: 'unknown' | 'retry' | 'failed', public code: string) { super(code); }
}

export interface MessageTransport {
  start(accept: (message: IncomingMessage) => boolean): void | Promise<void>;
  connectionStatus(): string;
  close(): void;
  onFatal?: () => void;
  reply(row: Outgoing): Promise<Delivery>;
  reconcile(row: Outgoing): Promise<Delivery | undefined>;
  download(messageId: string, fileKey: string): Promise<Buffer>;
  // Only channels with server-side send deduplication may retry an ambiguous delivery.
  readonly retryAmbiguous: boolean;
}
