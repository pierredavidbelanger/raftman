FROM alpine:3.6

ENTRYPOINT ["/usr/local/bin/raftman"]

COPY . /go/src/github.com/pierredavidbelanger/raftman

RUN apk --no-cache add -t build-deps build-base go git \
	&& apk --no-cache add ca-certificates \
	&& cd /go/src/github.com/pierredavidbelanger/raftman \
	&& export GOPATH=/go \
	&& export PATH=$PATH:$GOPATH/bin \
	&& make \
	&& cp ./raftman /usr/local/bin/raftman \
	&& rm -rf /go \
	&& apk del --purge build-deps
