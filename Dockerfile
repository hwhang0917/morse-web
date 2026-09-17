# syntax=docker/dockerfile:1
# Build stage runs on the host platform and cross-compiles; Go needs no QEMU.
FROM --platform=$BUILDPLATFORM golang:1.27.1-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG TARGETOS TARGETARCH TARGETVARIANT
# Static binary: no libc needed at runtime, so the final stage can be scratch.
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH GOARM=${TARGETVARIANT#v} \
    go build -trimpath -ldflags='-s -w' -o /morse-web .

FROM scratch
COPY --from=build /morse-web /morse-web
USER 65534:65534
EXPOSE 8080
ENTRYPOINT ["/morse-web"]
CMD ["-addr", ":8080"]
