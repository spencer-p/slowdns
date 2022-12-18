.PHONY: docker docker-run

docker:
	docker build . -t slowdns

docker-run: docker
	docker run \
		-p 127.0.0.1:53:8053/udp \
		--env DNSSERVERS=1.1.1.1,1.0.0.1 \
		slowdns
