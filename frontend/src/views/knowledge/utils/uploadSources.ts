import { fileSizeVerification } from '@/utils'
import { shouldRejectKnowledgeFileType } from '@/utils/fileTypeVerification'

const UNSUPPORTED_AUDIO_EXTENSIONS = new Set(['mp3', 'wav', 'm4a', 'flac', 'ogg', 'aac'])

export function filterUnsupportedAudioFiles(files: File[]): { validFiles: File[]; audioFilteredCount: number } {
  const validFiles = files.filter(file => !UNSUPPORTED_AUDIO_EXTENSIONS.has(getUploadFileExt(file)))
  return { validFiles, audioFilteredCount: files.length - validFiles.length }
}

export const UPLOAD_VIDEO_EXTENSIONS = ['mp4', 'mov', 'avi', 'mkv', 'webm', 'wmv', 'flv']

export function getUploadFileExt(file: File): string {
  const dot = file.name.lastIndexOf('.')
  if (dot < 0) return ''
  return file.name.substring(dot + 1).toLowerCase()
}

export function getUploadFileKey(file: File): string {
  const path = (file as File & { webkitRelativePath?: string }).webkitRelativePath || ''
  return `${path || file.name}\0${file.size}`
}

export interface FilterUploadFilesOptions {
  supportedFileTypes?: Set<string> | string[]
  fromFolder?: boolean
  multiFile?: boolean
}

export interface FilterUploadFilesResult {
  validFiles: File[]
  skippedCount: number
  videoFilteredCount: number
  hiddenFileCount: number
  unsupportedTypeCount: number
  audioFilteredCount: number
  unavailableEngineCount: number
  oversizedCount: number
}

export function filterUploadFiles(
  files: FileList | File[],
  options: FilterUploadFilesOptions = {},
): FilterUploadFilesResult {
  const list = Array.from(files)
  const dynamicTypesRaw = options.supportedFileTypes
    ? options.supportedFileTypes instanceof Set
      ? options.supportedFileTypes
      : new Set(options.supportedFileTypes)
    : undefined
  // An empty set means the parser-engine list hasn't loaded yet (race with the
  // async fetch on mount). Treat it as "unknown" and fall back to the default
  // whitelist instead of rejecting every file as unsupported.
  const dynamicTypes = dynamicTypesRaw && dynamicTypesRaw.size > 0 ? dynamicTypesRaw : undefined

  const validFiles: File[] = []
  let skippedCount = 0
  let videoFilteredCount = 0
  let hiddenFileCount = 0
  let unsupportedTypeCount = 0
  let audioFilteredCount = 0
  let unavailableEngineCount = 0
  let oversizedCount = 0
  const multiFile = options.multiFile ?? list.length > 1

  for (const file of list) {
    if (options.fromFolder) {
      const relativePath = (file as File & { webkitRelativePath?: string }).webkitRelativePath || file.name
      if (relativePath.split('/').some(part => part.startsWith('.'))) {
        hiddenFileCount++
        continue
      }
    }

    const fileExt = getUploadFileExt(file)
    if (UNSUPPORTED_AUDIO_EXTENSIONS.has(fileExt)) {
      audioFilteredCount++
      skippedCount++
      continue
    }
    if (UPLOAD_VIDEO_EXTENSIONS.includes(fileExt)) {
      videoFilteredCount++
      continue
    }

    if (shouldRejectKnowledgeFileType(file.name)) {
      unsupportedTypeCount++
      skippedCount++
      continue
    }
    if (dynamicTypes && shouldRejectKnowledgeFileType(file.name, dynamicTypes)) {
      unavailableEngineCount++
      skippedCount++
      continue
    }
    if (fileSizeVerification(file, multiFile)) {
      oversizedCount++
      skippedCount++
      continue
    }

    validFiles.push(file)
  }

  return { validFiles, skippedCount, videoFilteredCount, hiddenFileCount, unsupportedTypeCount, audioFilteredCount, unavailableEngineCount, oversizedCount }
}
