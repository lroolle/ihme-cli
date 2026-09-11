#!/usr/bin/env python3
"""Build the public site from an explicit file list; no third-party packages."""

import hashlib
import json
import re
import shutil
import subprocess
from html.parser import HTMLParser
from pathlib import Path
from urllib.parse import unquote, urlsplit

ROOT = Path(__file__).resolve().parents[1]
OUT = ROOT / '_site'
SITE_URL = 'https://lroolle.github.io/ihme-cli/'
REPO_URL = 'https://github.com/lroolle/ihme-cli/'
# Deliberate public entry points. Never recursively copy docs or the workspace.
PUBLIC_DOCS = (
    'README.md', 'docs/usage.md', 'docs/agents.md', 'skill/SKILL.md',
    'ROADMAP.md', 'AGENTS.md', 'pkg/agentkit/README.md', 'LICENSE',
    'SECURITY.md', 'CONTRIBUTING.md',
)
ASSETS = ('index.html', 'style.css', 'main.js', 'icon.svg')
LINK = re.compile(r'(?<!!)\[([^\]\n]+)\]\(([^\s)]+)\)')


class Page(HTMLParser):
    def __init__(self, content):
        super().__init__(convert_charrefs=True)
        self.ids = set()
        self.links = []
        self.feed(content)

    def handle_starttag(self, tag, attrs):
        attrs = dict(attrs)
        if 'id' in attrs:
            if attrs['id'] in self.ids:
                raise ValueError(f"duplicate HTML id: {attrs['id']}")
            self.ids.add(attrs['id'])
        for key in ('href', 'src'):
            if key in attrs:
                self.links.append(attrs[key])


def main():
    revision = subprocess.check_output(
        ['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True
    ).strip()
    index = (ROOT / 'llms.txt').read_text()
    entries = LINK.findall(index)
    targets = [target for _, target in entries]
    if index.splitlines()[0] != '# ihme' or not index.split('\n\n')[1].startswith('> '):
        raise ValueError('llms.txt must begin with a title and blockquote summary')
    if len(targets) != len(set(targets)) or set(targets) != set(PUBLIC_DOCS):
        raise ValueError('llms.txt and PUBLIC_DOCS must list the same unique entry points')
    if OUT.exists():
        shutil.rmtree(OUT)
    OUT.mkdir()

    def public_link(source, target):
        url = urlsplit(target)
        if url.scheme or url.netloc or not url.path:
            return target
        resolved = (ROOT / source).parent / unquote(url.path)
        relative = resolved.resolve().relative_to(ROOT).as_posix()
        if not resolved.is_file():
            raise ValueError(f'{source}: missing link target {target}')
        base = SITE_URL if relative in PUBLIC_DOCS else f'{REPO_URL}blob/{revision}/'
        return base + relative + (f'#{url.fragment}' if url.fragment else '')

    full = ['# ihme documentation\n']
    for title, filename in entries:
        source = ROOT / filename
        text = source.read_text()
        text = LINK.sub(lambda match: f'[{match[1]}]({public_link(filename, match[2])})', text)
        text = text.replace(f'{REPO_URL}blob/main/', f'{REPO_URL}blob/{revision}/')
        dest = OUT / filename
        dest.parent.mkdir(parents=True, exist_ok=True)
        dest.write_text(text)
        full.append(f'\n---\n\nSource: {SITE_URL}{filename}\n\n{text}')
    (OUT / 'llms.txt').write_text(index)
    (OUT / 'llms-full.txt').write_text(''.join(full))
    for filename in ASSETS:
        shutil.copyfile(ROOT / 'site' / filename, OUT / filename)
    page_path = OUT / 'index.html'
    page_path.write_text(page_path.read_text().replace(
        f'{REPO_URL}blob/main/', f'{REPO_URL}blob/{revision}/'
    ))
    page = Page(page_path.read_text())
    for target in page.links:
        url = urlsplit(target)
        if url.scheme or url.netloc:
            continue
        if url.path and not (OUT / unquote(url.path)).is_file():
            raise ValueError(f'index.html: missing asset {target}')
        if not url.path and url.fragment and url.fragment not in page.ids:
            raise ValueError(f'index.html: missing anchor {target}')
    hashes = {
        file.relative_to(OUT).as_posix(): hashlib.sha256(file.read_bytes()).hexdigest()
        for file in sorted(OUT.rglob('*')) if file.is_file()
    }
    (OUT / 'build.json').write_text(json.dumps(
        {'revision': revision, 'files': hashes}, sort_keys=True, indent=2
    ) + '\n')
    print(f'Built {len(hashes)} public files; links and agent index checked.')


if __name__ == '__main__':
    main()
