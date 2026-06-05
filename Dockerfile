FROM node:22-alpine AS web-build
WORKDIR /src/web
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.22-alpine AS go-build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY --from=web-build /src/web/dist ./web/dist
RUN CGO_ENABLED=0 go build -o /out/foliospace-library ./cmd/foliospace-reader

FROM alpine:3.20
WORKDIR /app
RUN apk add --no-cache ffmpeg poppler-utils su-exec && addgroup -S foliospace && adduser -S foliospace -G foliospace
COPY --from=go-build /out/foliospace-library /app/foliospace-library
COPY --from=web-build /src/web/dist /app/web/dist
COPY scripts/docker-entrypoint.sh /app/docker-entrypoint.sh
RUN mkdir -p /config /library && chown -R foliospace:foliospace /config /app
EXPOSE 8080
ENV FOLIOSPACE_CONFIG_DIR=/config
ENV FOLIOSPACE_LIBRARY_DIR=/library
ENV FOLIOSPACE_ADDR=:8080
ENTRYPOINT ["/app/docker-entrypoint.sh"]
CMD ["/app/foliospace-library"]
