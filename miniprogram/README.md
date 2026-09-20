# Reliquary Mini Program

This directory contains a WeChat Mini Program plugin for Reliquary. It gives mobile users a lightweight entry point to:

- configure a Reliquary API endpoint and tenant API key;
- list available knowledge bases;
- import a URL into a selected knowledge base;
- ask a selected knowledge base through Reliquary knowledge chat.

## Getting started

1. Open `miniprogram/` in WeChat DevTools.
2. Copy `project.private.config.json.example` to `project.private.config.json` and set your real Mini Program AppID. The shared `project.config.json` intentionally does not include an AppID to avoid forcing maintainers into a placeholder project.
3. Open the **Settings** tab and fill in:
   - API Base URL, for example `https://reliquary.example.com`;
   - API Key from the Reliquary tenant settings page.
4. Open the **Knowledge** tab, refresh knowledge bases, and select the target knowledge base.
5. Import a URL or switch to **Chat** to ask questions.

## Local development notes

- WeChat DevTools may block `localhost` requests when URL validation is enabled. For local testing, either disable domain validation in DevTools or expose Reliquary through a HTTPS development domain.
- In production Mini Programs, add the Reliquary API domain to the Mini Program request domain allowlist.
- The chat endpoint returns Server-Sent Events. The Mini Program client parses completed SSE text responses and displays accumulated `answer` chunks.

## Test

```bash
cd miniprogram
npm test
```
