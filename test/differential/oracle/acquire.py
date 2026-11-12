#!/usr/bin/env python3
# SPDX-License-Identifier: Apache-2.0
"""Verify exact archives/source pins, then copy approved installed package bytes.

Acquisition only: no package scripts, installs, transformations or execution.
Existing destinations are never overwritten. Run once from any directory.
"""
import hashlib
import io
import json
from pathlib import Path
import tarfile
from urllib.request import urlopen

ROOT = Path(__file__).resolve().parent
INSTALL = Path('/usr/local/lib/node_modules/@deepseek-ai/dsh')
COMMIT = 'fcc0ad462172522aaf5c93a7b2e01f6b9f0b0159'
RAW = f'https://raw.githubusercontent.com/deepseek-ai/deepseek-harness/{COMMIT}'
PACKAGES = [
    ('@deepseek-ai/cordis', '4.1.0', 'cordis', '3b0099a6aa9b0b52c3e199042acf6f1ae195c5690f7de11aa61171bca7752eca'),
    ('@deepseek-ai/cordis-plugin-loader', '1.1.0', 'loader', 'afb467fc85361e8ab99a88cdcbcf90ef3ffc270dfbe8c2e03f503bb4c899939f'),
    ('@deepseek-ai/cosmokit', '1.9.0', 'cosmokit', '6225043dbc7f6ff6c7cb104bc381b74c972577967ad827af26180719e17ff71d'),
    ('@deepseek-ai/schemastery', '3.19.0', 'schemastery', 'f702ce152aba393497c5d8d1258c6e602c7d85a22c3071b5fee6ccf0503e39e0'),
    ('@standard-schema/spec', '1.1.0', 'standard-schema-spec', 'a7cb7268be280ab518d450f8c3b07c86f23417225c0ab53691ed59b3067cceaf'),
]


def sha(data):
    return hashlib.sha256(data).hexdigest()


def fetch(url):
    with urlopen(url, timeout=45) as response:
        return response.read()


def main():
    planned = []
    manifests = []
    for name, version, dest, archive_sha in PACKAGES:
        archive_name = name.split('/')[-1]
        url = f'https://registry.npmjs.org/{name}/-/{archive_name}-{version}.tgz'
        archive = fetch(url)
        assert sha(archive) == archive_sha, f'archive mismatch: {name}'
        files = []
        with tarfile.open(fileobj=io.BytesIO(archive), mode='r:gz') as tar:
            for member in tar.getmembers():
                if member.isdir():
                    continue
                assert member.isfile(), f'non-regular archive member: {member.name}'
                rel = Path(member.name).relative_to('package')
                assert '..' not in rel.parts and not rel.is_absolute()
                local = INSTALL / 'node_modules' / name / rel
                assert not local.is_symlink(), f'symlink refused: {local}'
                data = local.read_bytes()
                assert data == tar.extractfile(member).read(), f'installed archive mismatch: {name}/{rel}'
                source_url = None
                if dest != 'standard-schema-spec' and rel.parts[0] == 'src':
                    source_url = f'{RAW}/vendor/{dest}/{rel.as_posix()}'
                    assert data == fetch(source_url), f'source pin mismatch: {name}/{rel}'
                target = ROOT / 'vendor' / dest / rel
                assert not target.exists(), f'existing destination refused: {target}'
                planned.append((target, data))
                kind = ('source' if rel.parts[0] == 'src' else 'runtime' if rel.suffix in ('.js', '.mjs', '.cjs') else 'notice' if rel.name == 'LICENSE' else 'package')
                row = {'path': target.relative_to(ROOT).as_posix(), 'bytes': len(data), 'sha256': sha(data), 'kind': kind}
                if source_url:
                    row['sourceURL'] = source_url
                files.append(row)
        package_json = json.loads((INSTALL / 'node_modules' / name / 'package.json').read_text())
        assert package_json['name'] == name and package_json['version'] == version
        manifests.append({'name': name, 'version': version, 'archiveURL': url, 'archiveSHA256': archive_sha, 'files': sorted(files, key=lambda x: x['path'])})
    deepseek = (INSTALL / 'LICENSE').read_bytes()
    assert sha(deepseek) == 'ebb4f09972aee8608be255debaf78451a68e95c290f55c240dec2ecfa16ea6be'
    assert deepseek == fetch(f'{RAW}/LICENSE')
    notice = ROOT / 'notices' / 'DeepSeek-MIT.txt'
    assert not notice.exists()
    planned.append((notice, deepseek))
    manifest = ROOT / 'manifest.json'
    assert not manifest.exists()
    record = {'schema': 's0-vendor-manifest/v1', 'sourceCommit': COMMIT, 'execution': 'unmodified published built JavaScript; no source transpilation', 'packages': manifests,
              'additionalNotices': [{'path': notice.relative_to(ROOT).as_posix(), 'bytes': len(deepseek), 'sha256': sha(deepseek), 'sourceURL': f'{RAW}/LICENSE'}]}
    # Exclusive creation protects previous evidence and partially completed runs.
    for target, data in planned:
        target.parent.mkdir(parents=True, exist_ok=True)
        with target.open('xb') as output:
            output.write(data)
    with manifest.open('x') as output:
        json.dump(record, output, indent=2)
        output.write('\n')
    print(json.dumps({'packages': len(manifests), 'files': len(planned), 'sourceFiles': sum(f['kind'] == 'source' for p in manifests for f in p['files']), 'manifestSHA256': sha(manifest.read_bytes())}))


if __name__ == '__main__':
    main()
