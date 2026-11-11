STANDARDS_DIR ?= $(wildcard $(CURDIR)/../go-standards)
include standard.mk

.PHONY: fuzz

# Content and cursor indexing must survive arbitrary input (TST-4).
fuzz:
	$(GO) test -run='^$$' -fuzz='^FuzzContent$$' -fuzztime=10s -parallel=4 .
	$(GO) test -run='^$$' -fuzz='^FuzzCursor$$' -fuzztime=10s -parallel=4 .
