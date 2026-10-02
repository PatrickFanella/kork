# syntax=docker/dockerfile:1
FROM node:22-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/zelda ./cmd/zelda \
 && mkdir -p /out/data/uploads

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/zelda /zelda
# Owned by the distroless nonroot user so a fresh volume is writable.
COPY --from=build --chown=65532:65532 /out/data /data
ENV ZELDA_UPLOAD_DIR=/data/uploads
VOLUME /data
EXPOSE 8080
USER nonroot
ENTRYPOINT ["/zelda"]
