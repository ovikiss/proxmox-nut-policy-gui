FROM golang:1.26-bookworm AS build
WORKDIR /src
ARG UI_SHARED_REPO=https://github.com/ovikiss/mikrotik-ui-shared.git
ARG UI_SHARED_REF=main
ARG UI_SHARED_REV=
ARG APP_VERSION=dev
COPY go.mod go.sum ./
RUN go mod download
COPY main.go .
COPY templates templates
COPY static/header-controls.json static/header-controls.json
COPY static/proxmox-nut-mark.png static/proxmox-nut-mark.png
COPY scripts/sync-ui-shared.sh scripts/sync-ui-shared.sh
RUN apt-get update && apt-get install -y --no-install-recommends git ca-certificates \
    && rm -rf /var/lib/apt/lists/*
RUN UI_SHARED_REPO="$UI_SHARED_REPO" UI_SHARED_REF="$UI_SHARED_REF" UI_SHARED_REV="$UI_SHARED_REV" sh scripts/sync-ui-shared.sh
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w -X main.version=$APP_VERSION" -o /out/proxmox-nut-gui .

FROM scratch
WORKDIR /app
COPY --from=build /out/proxmox-nut-gui /app/proxmox-nut-gui
COPY --from=build /src/static /app/static
COPY --from=build /src/templates /app/templates
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
VOLUME ["/data"]
EXPOSE 8080
ENTRYPOINT ["/app/proxmox-nut-gui"]
