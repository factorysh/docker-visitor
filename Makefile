build: vendor
	go build .

vendor:
	dep ensure

clean:
	rm -rf vendor