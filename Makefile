all: install

install:
	go install -v ./...

docker:
	docker build -t aleksi/telesock .
