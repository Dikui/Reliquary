/** TDesign icon names for agent / RAG pipeline tool steps. */
export function getAgentToolIconName(
  toolName: string,
): string {
  if (toolName === 'thinking') {
    return 'ai-search'
  }
  if (toolName === 'search_knowledge' || toolName === 'knowledge_search') {
    return 'data-search'
  }
  if (toolName === 'grep_chunks') {
    return 'search'
  }
  if (toolName === 'get_document_info' || toolName === 'list_knowledge_chunks') {
    return 'file-search'
  }
  if (toolName === 'get_document_content') {
    return 'file-search'
  }
  if (toolName === 'todo_write') {
    return 'task'
  }
  if (toolName === 'image_analysis' || toolName === 'query_understand') {
    return 'ai-search'
  }
  if (toolName === 'attachment_parsing') {
    return 'attach'
  }
  if (toolName.startsWith('mcp_')) {
    return 'terminal'
  }
  return 'file-paste'
}
