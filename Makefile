BINARY := jenkins-cli
LDFLAGS := -s -w

.PHONY: build build-linux clean install

build:
	go build -ldflags="$(LDFLAGS)" -o $(BINARY) .

build-linux:
	GOOS=linux GOARCH=amd64 go build -ldflags="$(LDFLAGS)" -o $(BINARY) .

install: build
	cp $(BINARY) /usr/local/bin/

clean:
	rm -f $(BINARY)
