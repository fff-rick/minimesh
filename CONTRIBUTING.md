# Contributing to MiniMesh

Issues and pull requests are welcome. MiniMesh is a learning project, so a
small, well-tested change with a clear explanation is preferred over a broad
refactor.

## Local checks

Use Go 1.23 or newer. Generated protobuf code is committed; regenerate it only
when a file under `api/proto/` changes.

```bash
make proto        # requires Docker; only when proto definitions change
make test
make vet
```

For a changed capability, run the narrowest relevant demo or test target. For
example, use `make demo-stage8` for Control Stream recovery and
`make demo-stage10` for observability.

## Pull requests

- Keep each pull request focused on one behavior or concern.
- Add or update tests for a behavior change.
- Do not commit local build output, benchmark artifacts, certificates, tokens,
  kubeconfig files, or `.env` files.
- Explain any change to a wire protocol, retry policy, security boundary, or
  deployment manifest in the pull request description.

## Commit messages

Use a short imperative subject, such as `fix: retain endpoints across control
plane restart` or `docs: clarify Stage 16 prerequisites`.
