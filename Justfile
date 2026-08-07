EXE:="update-docker-compose-projects"

build-all: #!/usr/bin/env bash
    for i in $(jq -r 'to_entries|.[].key' .mapping.json); do just build $i; done

copy hostname:
    scp {{EXE}} {{hostname}}:~/bin

build hostname:
    just build-with-arch "{{hostname}}" $(jq -r --arg name "{{hostname}}" '.[$name]' .mapping.json)

build-with-arch hostname arch: (_build-arch arch) (copy hostname) clean

_build-arch arch:
    CGO_ENABLED=0 GOOS=linux GOARCH={{arch}} go build -o {{EXE}} .

clean:
    rm -f {{EXE}}

test:
    go test -race ./...

coverage:
    go test -covermode=atomic -coverprofile=coverage.out ./...
    go tool cover -func=coverage.out
