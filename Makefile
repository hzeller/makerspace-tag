GO?=go

all: makerspace_tag

makerspace_tag : *.go
	$(GO) build

clean:
	rm -f makerspace_tag
