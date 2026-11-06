FROM node:24-alpine AS build
WORKDIR /app
RUN corepack enable && corepack prepare pnpm@11.18.0 --activate
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile
COPY . .
RUN pnpm build

# The default (non-Alpine) image is built FROM scratch, without a shell.
FROM joseluisq/static-web-server:2
USER 65532:65532
COPY --from=build /app/dist /public
ENV SERVER_PORT=80
ENV SERVER_ROOT=/public
EXPOSE 80
