# syntax=docker/dockerfile:1

FROM oven/bun:1.3.13 AS base
WORKDIR /app
ENV NEXT_TELEMETRY_DISABLED=1

FROM base AS deps
COPY package.json bun.lock turbo.json ./
COPY apps/web/package.json ./apps/web/package.json
COPY packages ./packages
RUN bun install --frozen-lockfile

FROM deps AS dev
COPY . .
WORKDIR /app/apps/web
EXPOSE 3000
CMD ["bun", "run", "dev"]

FROM base AS prod
ENV NODE_ENV=production
COPY --from=deps /app /app
COPY . .
WORKDIR /app/apps/web
EXPOSE 3000
CMD ["sh", "-lc", "bun run build && bunx next start -H 0.0.0.0 -p 3000"]
