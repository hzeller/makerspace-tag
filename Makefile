GO=/usr/local/go/bin/go

all: nfc

nfc : *.go
	$(GO) build

clean:
	rm -f nfc
