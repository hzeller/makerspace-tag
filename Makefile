all: nfc

nfc : *.go
	go build

clean:
	rm -f nfc
