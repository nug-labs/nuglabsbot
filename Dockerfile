FROM golang:1.24-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/nuglabsbot-v2 .

FROM alpine:3.20

WORKDIR /app
RUN apk add --no-cache ca-certificates

COPY --from=builder /out/nuglabsbot-v2 /app/nuglabsbot-v2
COPY --from=builder /src/assets /app/assets

# Bot uses Telegram long polling (getUpdates); no inbound HTTP port required.
CMD ["/app/nuglabsbot-v2"]
