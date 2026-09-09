# syntax=docker/dockerfile:1
# CMoA Monitor: adapter-node build of monitor/. It reads traces and relays
# chat to cmoa serve; it does not run a round itself.
FROM node:24-bookworm-slim AS build
WORKDIR /app
ENV COREPACK_ENABLE_DOWNLOAD_PROMPT=0
RUN corepack enable && corepack prepare pnpm@11.9.0 --activate
COPY package.json pnpm-lock.yaml .npmrc ./
RUN pnpm install --frozen-lockfile
COPY . .
RUN pnpm build

FROM node:24-bookworm-slim
WORKDIR /app
ENV NODE_ENV=production \
	HOST=127.0.0.1 \
	PORT=3999
COPY --from=build /app/package.json ./
COPY --from=build /app/build ./build
COPY --from=build /app/node_modules ./node_modules
EXPOSE 3999
CMD ["node", "build"]
