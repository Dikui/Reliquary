export type MentionItemType = 'kb' | 'file' | 'tag' | 'mcp';

export interface MentionItem {
  id: string;
  name: string;
  type: MentionItemType;
  group?: string;
  description?: string;
  kbType?: 'document';
  count?: number;
  kbName?: string;
  kbId?: string;
  serviceId?: string;
  serviceName?: string;
  isAgentConfigured?: boolean;
}

export interface MentionRequestItem {
  id: string;
  name: string;
  type: MentionItemType;
  kb_type?: 'document';
  kb_id?: string;
  kb_name?: string;
  service_id?: string;
}
