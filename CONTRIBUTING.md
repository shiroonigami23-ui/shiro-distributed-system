# Contributing

Thank you for contributing to Shiro Distributed System.

## Workflow
1. Fork the repository.
2. Create a feature branch from `main`.
3. Make focused changes with clear commit messages.
4. Run local checks before opening a PR.
5. Open a pull request with a concise summary, motivation, and test evidence.

## Local Checks
```bash
go mod tidy
go test ./...
```

## Development Standards
- Keep changes modular and production-focused.
- Preserve backward compatibility for public API contracts when possible.
- Add or update docs for behavior changes.
- Prefer explicit configuration over hardcoded behavior.
- Keep logs structured and actionable.

## Pull Request Requirements
- Problem statement and design summary.
- Security impact notes (auth, ACL, mTLS, data flow).
- Operational impact notes (deployments, scaling, observability).
- Test coverage or reproduction steps.

## Security Issues
Do not open public issues for sensitive vulnerabilities. Report responsibly to repository maintainers.
