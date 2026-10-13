export interface EntryDownloadItem {
  id: string;
  original_file_name?: string;
  file_name?: string;
  title?: string;
  type?: string;
}

export function resolveEntryDownloadFileName(item: EntryDownloadItem): string {
  const baseName = item.original_file_name || item.file_name || item.title || item.id;
  if (item.type === 'manual' && !baseName.toLowerCase().endsWith('.md')) {
    return `${baseName}.md`;
  }
  return baseName;
}
