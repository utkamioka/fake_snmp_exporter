VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags="-X main.version=$(VERSION)"

.PHONY: build
build:
	go build $(LDFLAGS) -o fake_snmp_exporter .

.PHONY: install
install:
	go install $(LDFLAGS) .
