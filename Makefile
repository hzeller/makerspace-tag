all: nfc

% : %.go
	go build $^

clean:
	rm -f nfc
