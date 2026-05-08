# syntax=docker/dockerfile:1

FROM golang:1.24-alpine AS base
WORKDIR /app/services/server
RUN apk add --no-cache ca-certificates
COPY services/server/go.mod services/server/go.sum ./
RUN go mod download

FROM base AS dev
COPY services/server ./
EXPOSE 9000
CMD ["go", "run", "main.go"]

FROM base AS build
COPY services/server ./
RUN CGO_ENABLED=0 GOOS=linux go build -o /usr/local/bin/riverline-server ./main.go

FROM alpine:3.22 AS prod
RUN apk add --no-cache ca-certificates
COPY --from=build /usr/local/bin/riverline-server /usr/local/bin/riverline-server
EXPOSE 9000
CMD ["riverline-server"]
