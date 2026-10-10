import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'

const botMessage = readFileSync(new URL('./botmsg.vue', import.meta.url), 'utf8')
const agentStream = readFileSync(new URL('./AgentStreamDisplay.vue', import.meta.url), 'utf8')
const chatView = readFileSync(new URL('../index.vue', import.meta.url), 'utf8')
const sharedStyles = readFileSync(
  new URL('../../../components/css/chat-message-shared.less', import.meta.url),
  'utf8',
)

test('non-agent actions wait for the typewriter buffer to finish', () => {
  assert.match(botMessage, /const answerFullyRendered = computed/)
  assert.match(botMessage, /typedAnswer\.value\.length >= answerText\.value\.length/)
  assert.match(botMessage, /v-if="answerFullyRendered && \(content \|\| session\.content\)"/)
})

test('agent actions reuse the fully-rendered answer state', () => {
  assert.match(agentStream, /const answerFullyRendered = computed/)
  assert.match(agentStream, /typedAnswer\.value\.length >= activeAnswerMarkdown\.value\.length/)
  assert.match(agentStream, /v-if="answerFullyRendered && event\.done/)
})

test('artifact count is a top-right overlay instead of t-badge', () => {
  assert.match(botMessage, /class="answer-toolbar__artifact"/)
  assert.match(agentStream, /class="answer-toolbar__artifact"/)
  assert.match(botMessage, /class="answer-toolbar__artifact-count"/)
  assert.match(agentStream, /class="answer-toolbar__artifact-count"/)
  assert.match(sharedStyles, /answer-toolbar__artifact-count/)
  assert.match(sharedStyles, /position: absolute/)
  assert.match(sharedStyles, /td-brand-color/)
  assert.match(sharedStyles, /artifact-toolbar-arrive/)
  assert.match(botMessage, /is-arrived': artifactArrived/)
  assert.match(agentStream, /is-arrived': artifactArrived/)
  assert.match(botMessage, /useArtifactArriveMotion/)
  assert.match(agentStream, /useArtifactArriveMotion/)
  assert.doesNotMatch(botMessage, /<t-badge/)
  assert.doesNotMatch(agentStream, /<t-badge/)
})

test('artifact toolbar uses a folder icon for stored message files', () => {
  assert.match(botMessage, /answer-toolbar__artifact[\s\S]{0,800}name="folder"/)
  assert.match(agentStream, /answer-toolbar__artifact[\s\S]{0,800}name="folder"/)
  assert.doesNotMatch(botMessage, /answer-toolbar__artifact[\s\S]{0,800}:loading=/)
  assert.doesNotMatch(agentStream, /answer-toolbar__artifact[\s\S]{0,800}:loading=/)
})

test('conversation timestamps insert into the message flow instead of each bubble', () => {
  assert.match(chatView, /shouldShowConversationTimestamp\(messagesList, index\)/)
  assert.doesNotMatch(chatView, /align="end"/)
  assert.doesNotMatch(chatView, /align="start"/)
})
