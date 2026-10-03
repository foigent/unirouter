# Build stage
FROM node:24.19.0-alpine3.23@sha256:244cc2b53f46f9e876304391d17682b0ddae9ac33491f4857e25e35a36ba7995 AS builder
WORKDIR /app
ARG VITE_DEPLOY_ENV=
ARG VITE_APP_VERSION=
ENV VITE_DEPLOY_ENV=${VITE_DEPLOY_ENV}
ENV VITE_APP_VERSION=${VITE_APP_VERSION}
COPY package.json package-lock.json ./
COPY scripts/check-toolchain.mjs ./scripts/check-toolchain.mjs
COPY Dockerfile ./Dockerfile
RUN node scripts/check-toolchain.mjs
RUN npm pkg delete devDependencies.canvas && npm ci
COPY . .
RUN npm run build

# Production stage
FROM nginx:1.30.3-alpine
COPY --from=builder /app/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 80
CMD ["nginx", "-g", "daemon off;"]
