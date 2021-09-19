GO=/usr/local/go/bin/go

all: makerspace_tag

makerspace_tag : *.go
	$(GO) build

clean:
	rm -f nfc
