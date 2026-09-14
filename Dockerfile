# syntax=docker/dockerfile:1
FROM golang:1.24 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/external-dns-bluecat-webhook ./cmd/webhook

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/external-dns-bluecat-webhook /external-dns-bluecat-webhook
USER nonroot:nonroot
ENTRYPOINT ["/external-dns-bluecat-webhook"]
