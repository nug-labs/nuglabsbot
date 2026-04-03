FROM golang:1.23-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/telegram-v2 .

FROM alpine:3.20

WORKDIR /app
RUN apk add --no-cache ca-certificates

COPY --from=builder /out/telegram-v2 /app/telegram-v2
COPY --from=builder /src/assets /app/assets

# APP_ENV comes from --env-file (live: APP_ENV=live). Unset defaults to test file semantics in Env.Init for local .env files.
CMD ["/app/telegram-v2"]
