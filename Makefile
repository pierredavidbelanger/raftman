all: get dep gen build

clean:
	rm -rf vendor frontend/static.go raftman

get:
	go get -v -u github.com/golang/dep/cmd/dep \
	&& go get -v -u github.com/mjibson/esc

dep:
	dep ensure

gen:
	esc -o frontend/static.go -pkg frontend frontend/static

build:
	go build -v

run:
	./raftman -backend sqlite:///tmp/logs.db -frontend syslog+tcp://:5514?format=RFC3164 -frontend syslog+udp://:5514?format=RFC3164 -frontend ui+http://:8282/

install:
	go install -v

image:
	docker build -t raftman .
