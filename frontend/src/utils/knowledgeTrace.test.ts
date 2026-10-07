import assert from 'node:assert/strict'
import test from 'node:test'

import {
  summarizePostprocessTasks,
  type KnowledgeTraceNode,
} from './knowledgeTrace.ts'

test('summarizes asynchronous postprocess leaf tasks', () => {
  const trace: KnowledgeTraceNode = {
    name: 'root',
    kind: 'root',
    status: 'done',
    children: [
      {
        name: 'postprocess',
        kind: 'stage',
        status: 'done',
        children: [
          { name: 'postprocess.summary', kind: 'subspan', status: 'done' },
          { name: 'postprocess.question', kind: 'subspan', status: 'running' },
          {
            name: 'postprocess.multimodal',
            kind: 'group',
            status: 'failed',
            children: [
              { name: 'postprocess.multimodal.image[0]', kind: 'subspan', status: 'failed' },
              { name: 'postprocess.multimodal.image[1]', kind: 'subspan', status: 'pending' },
            ],
          },
        ],
      },
    ],
  }

  assert.deepEqual(summarizePostprocessTasks(trace), {
    running: 2,
    failed: 1,
    completed: 1,
    other: 0,
    total: 4,
  })
})

test('returns an empty postprocess summary when no trace is available', () => {
  assert.deepEqual(summarizePostprocessTasks(undefined), {
    running: 0,
    failed: 0,
    completed: 0,
    other: 0,
    total: 0,
  })
})
