TAG = v0.2.0
PREFIX = staging-k8s.gcr.io

build: server_status_exporter

server_status_exporter: server_status_exporter.go
	go build -a -o server_status_exporter server_status_exporter.go

clean:
	rm -rf sd_dummy_exporter