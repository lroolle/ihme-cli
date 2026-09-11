#!/usr/bin/env python3
"""Verify the public deployment, including bytes, against the local build."""

import argparse
import hashlib
import json
import time
from pathlib import Path
from urllib.error import URLError
from urllib.request import Request, urlopen


def fetch(base, path, revision):
    request = Request(
        f'{base.rstrip("/")}/{path}?revision={revision}',
        headers={'Cache-Control': 'no-cache', 'User-Agent': 'ihme-pages-check'},
    )
    with urlopen(request, timeout=15) as response:
        return response.read()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('url')
    parser.add_argument('--site', type=Path, default=Path('_site'))
    parser.add_argument('--attempts', type=int, default=1)
    args = parser.parse_args()
    if args.attempts < 1:
        parser.error('--attempts must be positive')
    expected = json.loads((args.site / 'build.json').read_text())
    for attempt in range(args.attempts):
        try:
            actual = json.loads(fetch(args.url, 'build.json', expected['revision']))
            if actual != expected:
                raise ValueError('published manifest differs from this build')
            for path, digest in expected['files'].items():
                if hashlib.sha256(fetch(args.url, path, expected['revision'])).hexdigest() != digest:
                    raise ValueError(f'published file differs from this build: {path}')
            print(f'Published {expected["revision"]}: all {len(expected["files"])} files match.')
            return
        except (URLError, ValueError, TimeoutError) as error:
            if attempt == args.attempts - 1:
                raise SystemExit(f'Pages verification failed: {error}') from error
            print(f'Waiting for Pages: {error}', flush=True)
            time.sleep(5)


if __name__ == '__main__':
    main()
