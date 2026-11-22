FROM node:26.12.1-bookworm-slim@sha256:88e480776c6149ff1d098cd304f44bf18057fb5d284a3329b78162d29821e28c AS build
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY --from=channels /package.json /package-lock.json ./integration/
RUN npm ci --prefix integration --omit=dev --no-audit --no-fund && mkdir -p integration/node_modules
COPY --from=channels /*.ts ./integration/
COPY *.ts tsconfig*.json ./
RUN npm run build && npm prune --omit=dev --no-audit --no-fund

FROM node:26.12.1-bookworm-slim@sha256:88e480776c6149ff1d098cd304f44bf18057fb5d284a3329b78162d29821e28c
ENV NODE_ENV=production AGENT_DATA_DIR=/data
WORKDIR /app
COPY --from=build --chown=node:node /app/package.json ./
COPY --from=build --chown=node:node /app/node_modules ./node_modules
COPY --from=build --chown=node:node /app/dist ./dist
COPY --from=build --chown=node:node /app/integration/node_modules ./dist/integration/node_modules
RUN mkdir /data && chown node:node /data
USER node
HEALTHCHECK --interval=15s --timeout=5s --start-period=30s --retries=3 CMD ["node", "--input-type=module", "-e", "const r=await fetch('http://127.0.0.1:8787/ready');process.exit(r.ok?0:1)"]
CMD ["node", "--use-env-proxy", "dist/integration/main.js"]
