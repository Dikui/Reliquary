import assert from 'node:assert/strict';
import test from 'node:test';

import { resolveEntryDownloadFileName } from './entryDownloadFileName.ts';

test('prefers the original filename over the extensionless display name', () => {
  assert.equal(resolveEntryDownloadFileName({
    id: 'entry-1',
    original_file_name: 'quarterly-report.pdf',
    file_name: 'quarterly-report',
    type: 'file',
  }), 'quarterly-report.pdf');
});

test('falls back to the backend filename when no original filename is present', () => {
  assert.equal(resolveEntryDownloadFileName({
    id: 'entry-2',
    file_name: 'notes.txt',
    type: 'file',
  }), 'notes.txt');
});

test('adds the markdown extension to manual documents exactly once', () => {
  assert.equal(resolveEntryDownloadFileName({
    id: 'entry-3',
    title: 'meeting-notes',
    type: 'manual',
  }), 'meeting-notes.md');
  assert.equal(resolveEntryDownloadFileName({
    id: 'entry-4',
    title: 'meeting-notes.md',
    type: 'manual',
  }), 'meeting-notes.md');
});
