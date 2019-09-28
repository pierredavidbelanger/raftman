# raftman

![raftman](https://raw.githubusercontent.com/pierredavidbelanger/raftman/master/frontend/static/ui/logo-96.png)

A syslog server with integrated full text search via a JSON API and Web UI.

- [getting started](#getting-started)
- [configuration](#configuration)

## getting started

### store logs

To get started quickly, just run the containerized version of raftman:

```
sudo docker run --rm --name raftman \
    -v /tmp:/var/lib/raftman \
    -p 514:514/udp \
    -p 5514:5514 \
    -p 8181:8181 \
    -p 8282:8282 \
    pierredavidbelanger/raftman
```


This will start raftman with all default options. It listen on port 514 (UDP) and 5514 (TCP) on the host for incoming RFC5424 syslog packets and store them into an SQLite database stored in `/tmp/logs.db` on the host. It also exposes the JSON API on http://localhost:8181/api/ and the Web UI on http://localhost:8282/.

### send logs

Time to fill our database. The easyest way is to just start [logspout](https://github.com/gliderlabs/logspout) and tell it to point to raftman's syslog port:

```
docker run --rm --name logspout \
    -v /var/run/docker.sock:/var/run/docker.sock:ro \
    --link raftman \
    gliderlabs/logspout \
        syslog://raftman:514
```


This last container will grab other containers output lines and send them as syslog packet to the configured syslog server (ie: our linked raftman container).

### generate logs

Now, we also need to generate some output. This will do the job for now:

```
docker run --rm --name test \
    alpine \
    echo 'Can you see me'
```


### visualise logs

Then we can visualize our logs:

with the raftman API:

```
curl http://localhost:8181/api/list \
    -d '{"Limit": 100, "Message": "see"}'
```


or pop the Web UI at http://localhost:8282/

## configuration

All raftman configuration options are set as arguments in the command line.

For example, here is the what the command line would looks like if we set all the default values explicitly:

```
raftman \
    -backend sqlite:///var/lib/raftman/logs.db?insertQueueSize=512&queryQueueSize=16&timeout=5s&batchSize=32&retention=INF \
    -frontend syslog+udp://:514?format=RFC5424&queueSize=512&timeout=0s \
    -frontend syslog+tcp://:5514?format=RFC5424&queueSize=512&timeout=0s \
    -frontend api+http://:8181/api/ \
    -frontend ui+http://:8282/
```
