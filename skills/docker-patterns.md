---
name: docker-patterns
description: Docker and Docker Compose patterns for local dev and production.
---
# Docker Patterns
Docker best practices:

1. **Multi-stage builds**: Separate build and runtime images.
2. **Non-root user**: Run containers as a non-root user.
3. **Health checks**: Add HEALTHCHECK to every service.
4. **Proper signal handling**: Use exec form (`CMD ["app"]`) for PID 1.
5. **Layer caching**: Copy dependency files first, then source.
6. **Secrets**: Never bake secrets into images — use secrets management.
7. **Resource limits**: Set memory/CPU limits in compose files.
8. **.dockerignore**: Exclude node_modules, .git, build artifacts.
