ifneq (,$(wildcard .secrets/dev.mk))
    include .secrets/dev.mk
endif


gen:
	go run ./cmd/opendatalab -save=./download.txt

download:
	go run ./cmd/download -in-file=download.txt -task-num=10