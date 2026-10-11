import rssIcon from '@/assets/img/datasource-rss.svg'

export function getDatasourceIconUrl(type: string): string | undefined {
  return type === 'rss' ? rssIcon : undefined
}
