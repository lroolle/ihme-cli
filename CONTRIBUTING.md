# Contributing

Small fixes and reproducible bug reports are welcome. Open an issue before starting a larger feature so we can agree on the behavior.

## Report a bug

Include your OS, `ihme version`, the command, what you expected, and the redacted error. Do not attach session files, cookies, model keys, or account exports. For vulnerabilities, follow [Security](SECURITY.md).

## Change the code

Install the Go version in `go.mod`, then run:

```bash
make check
```

Use fixtures and local mock servers for tests. A test must not need someone's Apple account or a paid model request. Add a regression test when fixing behavior and state what you actually ran in the PR.

## Change the docs or website

The README is the entry point; `docs/usage.md` and `docs/agents.md` hold the detail. Keep examples consistent with `ihme <command> --help` and label invented demo data.

```bash
make site
python3 -m http.server 8000 --directory _site
```

`site/` contains the landing page. `scripts/build_site.py` copies an explicit set of public files into `_site/`, checks local links, and derives `llms-full.txt` from the root `llms.txt` map. Update that map when an entry point changes. Never put account data, screenshots from real sessions, or local machine paths into the site.

The Pages workflow checks pull requests and publishes changes on `main`. A deployment succeeds only after the public `build.json` identifies the deployed commit. Generated site output stays out of source control.
