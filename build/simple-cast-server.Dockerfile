# syntax=docker/dockerfile:1

# ---- build ----
FROM --platform=$BUILDPLATFORM golang:1.24 AS build
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags="-s -w" \
    -o /out/simple-cast-server ./cmd/simple-cast-server

# ---- runtime ----
# Fully static binary (CGO disabled), so distroless static is enough - no
# libc, no shell, no package manager in the final image.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/simple-cast-server /usr/local/bin/simple-cast-server
ENTRYPOINT ["/usr/local/bin/simple-cast-server"]
