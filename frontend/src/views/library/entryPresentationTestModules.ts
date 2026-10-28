import { loadUiComponent } from '../../components/ui/uiTestLoader'
import * as processing from '../../utils/entryProcessingStatus'
import * as feedback from '../../components/ui/feedback'

/** Production presentation components for SFC unit loaders; no browser is used. */
export const entryPresentationTestModules: Record<string, unknown> = {
  '@/components/ui': Object.fromEntries(['Button', 'Input', 'Select', 'Chip', 'LibMark', 'Skeleton', 'EmptyState', 'ErrorState', 'DataTable', 'Tag', 'StatusSquare'].map(name => [name, loadUiComponent(name)])),
  '@/utils/entryProcessingStatus': processing,
  '@/components/ui/feedback': feedback,
}
