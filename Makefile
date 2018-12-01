TAG = v0.1.3
PREFIX = springmt

build: server_status_exporter

server_status_exporter: server_status_exporter.go
	GOOS=linux GOARCH=amd64 go build -a -o server_status_exporter server_status_exporter.go

docker: server_status_exporter
	docker build --pull -t ${PREFIX}/rack-server-status-to-sd:$(TAG) .

push: docker
	docker push ${PREFIX}/rack-server-status-to-sd:$(TAG)

clean:
	rm -rf server_status_exporter
