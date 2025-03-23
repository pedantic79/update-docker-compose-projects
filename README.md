# Update Docker Compose Projects

This basically does the following:

1. `docker compose ls`
2. For every project, docker compose pull
3. If there was an updated image, it restarts the project
