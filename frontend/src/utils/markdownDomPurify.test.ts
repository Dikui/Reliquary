import assert from 'node:assert/strict';
import test from 'node:test';
import {
  domPurifyAllowedUriRegexp,
  markdownDomPurifyConfig,
  markdownDomPurifySecurityHooks,
} from './markdownDomPurify.ts';

test('markdownDomPurifyConfig FORBID_TAGS includes script', () => {
  assert.ok(Array.isArray(markdownDomPurifyConfig.FORBID_TAGS));
  assert.ok(markdownDomPurifyConfig.FORBID_TAGS.includes('script'));
});

test('ALLOWED_URI_REGEXP allows local:// and vault:// and rejects javascript:', () => {
  const re = markdownDomPurifyConfig.ALLOWED_URI_REGEXP ?? domPurifyAllowedUriRegexp;
  assert.match('local://1/exports/image.png', re);
  assert.match('vault://article-id/..%2F..%2Fattachments%2Farticle-id%2Fimage.png', re);
  assert.doesNotMatch('javascript:alert(1)', re);
});

test('markdown links always open in a new tab', () => {
  const attributes = new Map([['href', '/platform/libraries/library-1']]);
  const anchor = {
    tagName: 'A',
    getAttribute: (name: string) => attributes.get(name) ?? null,
    setAttribute: (name: string, value: string) => attributes.set(name, value),
    hasAttribute: (name: string) => attributes.has(name),
    removeAttribute: (name: string) => attributes.delete(name),
  };

  markdownDomPurifySecurityHooks.afterSanitizeElements(anchor as unknown as Node);

  assert.equal(attributes.get('target'), '_blank');
  assert.equal(attributes.get('rel'), 'noopener noreferrer');
});
