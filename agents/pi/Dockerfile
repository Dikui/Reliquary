FROM node:26.12.1-bookworm-slim@sha256:88e480776c6149ff1d098cd304f44bf18057fb5d284a3329b78162d29821e28c
# The launcher runs Pi as the host user with /data mounted from the runtime directory.
ENV NODE_ENV=production HOME=/data PI_CODING_AGENT_DIR=/data/agent PI_SKIP_VERSION_CHECK=1 PI_TELEMETRY=0
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci --omit=dev --no-audit --no-fund && npm cache clean --force
COPY reliquary-upload.ts ./
WORKDIR /workspace
ENTRYPOINT ["node", "--use-env-proxy", "/app/node_modules/@earendil-works/pi-coding-agent/dist/bundle/cli.js"]
