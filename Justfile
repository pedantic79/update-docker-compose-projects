EXE:="update-docker-compose-projects"

clean:
    rm -f {{EXE}}

build arch:
    CGO_ENABLED=0 GOOS=linux GOARCH={{arch}} go build -o {{EXE}} .

copy hostname:
    scp {{EXE}} {{hostname}}:~/bin

build-pipeline hostname arch: (build arch) (copy hostname) clean

pipeline hostname:
    just build-pipeline "{{hostname}}" $(jq -r --arg name "{{hostname}}" '.[$name]' .mapping.json)

build-all: #!/usr/bin/env bash
    for i in $(jq -r 'to_entries|.[].key' .mapping.json); do just pipeline $i; done
