EXE:="update-docker-compose-projects"

build-all: #!/usr/bin/env bash
    for i in $(jq -r 'to_entries|.[].key' .mapping.json); do just build $i; done

clean:
    rm -f {{EXE}}

build-with-arch hostname arch: (_build-arch arch) (copy hostname) clean

_build-arch arch:
    CGO_ENABLED=0 GOOS=linux GOARCH={{arch}} go build -o {{EXE}} .

copy hostname:
    scp {{EXE}} {{hostname}}:~/bin

build hostname:
    just build-with-arch "{{hostname}}" $(jq -r --arg name "{{hostname}}" '.[$name]' .mapping.json)
