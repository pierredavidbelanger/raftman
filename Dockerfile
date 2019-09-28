FROM golang:1.13-alpine3.10 AS golang
WORKDIR /src
RUN apk --no-cache add build-base git \
    && GO111MODULE=off go get github.com/mjibson/esc
COPY . ./
RUN go generate && go build

FROM scratch
ENTRYPOINT ["/usr/local/bin/raftman"]
COPY --from=golang /src/raftman /usr/local/bin/raftman