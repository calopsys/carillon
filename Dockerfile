# syntax=docker/dockerfile:1

# --- build -------------------------------------------------------------------
FROM golang:1.26.4-alpine AS build
WORKDIR /src

# CA bundle for the scratch runtime (HTTPS to GitHub/GitLab/registries/webhook).
RUN apk add --no-cache ca-certificates

# Cache module downloads.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG VERSION=dev
# timetzdata embeds the zoneinfo DB so the binary is self-contained on scratch.
RUN CGO_ENABLED=0 go build -tags timetzdata \
      -ldflags "-s -w -X github.com/calopsys/carillon/internal/cli.buildVersion=${VERSION}" \
      -o /out/carillon ./cmd/carillon

# --- runtime -----------------------------------------------------------------
# scratch: nothing but the static binary and a CA bundle. No shell, no libc.
FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/carillon /usr/local/bin/carillon
# Numeric nonroot UID:GID (no /etc/passwd on scratch; matches the k8s securityContext).
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/carillon"]
CMD ["run"]
