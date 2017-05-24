FROM golang:1.8

RUN https_proxy=http://proxy:8080 go get github.com/MichaelSp/influxdb-firehose-nozzle
ADD influxdb-firehose-nozzle.json ./influxdb-firehose-nozzle.json

CMD ["/go/bin/influxdb-firehose-nozzle", "-config", "influxdb-firehose-nozzle.json"]
