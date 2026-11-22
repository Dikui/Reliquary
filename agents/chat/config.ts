import { isAbsolute, join, resolve } from 'node:path';
import { QueryError } from './output.js';

export type Config = ReturnType<typeof loadConfig>;
export function loadDataDir(env: NodeJS.ProcessEnv = process.env) {
  const path = env.AGENT_DATA_DIR?.trim();
  if (!path || !isAbsolute(path)) throw new QueryError('invalid_config', 'AGENT_DATA_DIR must be absolute');
  return resolve(path);
}

export function loadConfig(env: NodeJS.ProcessEnv = process.env) {
  const required = (name: string) => {
    const value = env[name]?.trim();
    if (!value) throw new QueryError('invalid_config', `Missing configuration: ${name}`);
    return value;
  };
  const endpoint = (name: string) => {
    let url: URL;
    try { url = new URL(required(name)); }
    catch { throw new QueryError('invalid_config', `Invalid endpoint: ${name}`); }
    if (!['http:', 'https:'].includes(url.protocol) || url.username || url.password || url.search || url.hash) {
      throw new QueryError('invalid_config', `Invalid endpoint: ${name}`);
    }
    return url.href.replace(/\/$/, '');
  };
  const integer = (name: string, fallback?: number) => {
    const value = env[name] ?? fallback?.toString();
    if (!value || !/^\d+$/.test(value) || !Number.isSafeInteger(Number(value)) || Number(value) < 1) {
      throw new QueryError('invalid_config', `Invalid positive integer: ${name}`);
    }
    return Number(value);
  };
  const libraries = [...new Set(required('RELIQUARY_LIBRARY_IDS').split(',').map(id => id.trim()).filter(Boolean))];
  if (!libraries.length) throw new QueryError('invalid_config', 'RELIQUARY_LIBRARY_IDS must contain a library ID');
  if (env.MODEL_SOURCE && !['api', 'chatgpt'].includes(env.MODEL_SOURCE.trim())) {
    throw new QueryError('invalid_config', 'MODEL_SOURCE must be api or chatgpt');
  }
  const apiURL = endpoint('RELIQUARY_API_URL');
  const modelAPI = env.MODEL_SOURCE?.trim() === 'chatgpt' ? 'openai-codex-responses'
    : required('MODEL_API');
  if (!['openai-completions', 'openai-responses', 'anthropic-messages', 'openai-codex-responses'].includes(modelAPI)) {
    throw new QueryError('invalid_config', 'Unsupported MODEL_API');
  }
  const subscription = modelAPI === 'openai-codex-responses';
  const codexAuthFile = env.CODEX_AUTH_FILE?.trim();
  if (codexAuthFile && (!subscription || !isAbsolute(codexAuthFile))) {
    throw new QueryError('invalid_config', 'CODEX_AUTH_FILE requires an absolute path and the Codex subscription protocol');
  }
  const contextWindow = integer('MODEL_CONTEXT_WINDOW', subscription ? 128000 : undefined);
  const maxTokens = integer('MODEL_MAX_TOKENS', subscription ? 8192 : undefined);
  if (maxTokens >= contextWindow) throw new QueryError('invalid_config', 'MODEL_MAX_TOKENS must be less than MODEL_CONTEXT_WINDOW');
  const configuredThinkingLevel = env.MODEL_THINKING_LEVEL?.trim() || 'off';
  const thinkingLevel = (['off', 'minimal', 'low', 'medium', 'high', 'xhigh', 'max'] as const)
    .find(level => level === configuredThinkingLevel);
  if (!thinkingLevel) throw new QueryError('invalid_config', 'Invalid MODEL_THINKING_LEVEL');
  return {
    apiURL,
    apiKey: required('RELIQUARY_API_KEY'),
    libraries,
    modelURL: subscription ? 'https://chatgpt.com/backend-api' : endpoint('MODEL_BASE_URL'),
    modelKey: subscription ? '' : required('MODEL_API_KEY'),
    authFile: join(loadDataDir(env), 'auth.json'),
    codexAuthFile,
    model: env.MODEL_NAME?.trim() || (subscription ? 'gpt-6.1-sol' : required('MODEL_NAME')),
    modelAPI: modelAPI as 'openai-completions' | 'openai-responses' | 'anthropic-messages' | 'openai-codex-responses',
    thinkingLevel,
    contextWindow,
    maxTokens,
    dataDir: loadDataDir(env),
    timeoutMs: integer('AGENT_TIMEOUT_MS', 120000),
  };
}
