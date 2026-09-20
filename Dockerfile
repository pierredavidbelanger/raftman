FROM golang:1.27-alpine AS build
RUN apk --no-cache add build-base
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=1 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /raftman ./cmd/raftman

FROM alpine:3.22
RUN mkdir -p /var/lib/raftman
COPY --from=build /raftman /usr/local/bin/raftman
EXPOSE 514/udp 5514 8181 8282
ENTRYPOINT ["/usr/local/bin/raftman"]
