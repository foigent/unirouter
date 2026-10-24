#!/usr/bin/env python3
"""Select installation references from pinned public metadata, without Docker or signing.

The prior accepted installation descriptor supplies the four unchanged upstream
references. A pinned publisher receipt supplies the two app references. Native
Docker observations must confirm each exact reference; build pull labels and
obsolete templates cannot silently replace those observations.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import sys

ROLES = {'backend', 'frontend', 'postgres', 'redis', 'prometheus', 'grafana'}
APPS = {'backend', 'frontend'}
SCHEMA = 'unirouter.install.v1'


class DescriptorError(Exception):
    pass


def require(ok, message):
    if not ok:
        raise DescriptorError(message)


def strict_json(raw):
    def pairs(items):
        result = {}
        for key, value in items:
            require(key not in result, 'Duplicate metadata field.')
            result[key] = value
        return result
    return json.loads(raw, object_pairs_hook=pairs)


def digest_reference(value):
    return isinstance(value, str) and re.fullmatch(r'[a-z0-9][a-z0-9./:_-]*@sha256:[a-f0-9]{64}', value) is not None


def select_descriptor(previous, app_references, observations):
    require(isinstance(previous, dict) and set(previous) == {'schema_version', 'images'} and
            previous['schema_version'] == SCHEMA and isinstance(previous['images'], dict) and
            set(previous['images']) == ROLES and all(digest_reference(x) for x in previous['images'].values()),
            'Prior accepted descriptor is incomplete.')
    require(isinstance(app_references, dict) and set(app_references) == APPS and
            all(digest_reference(x) for x in app_references.values()), 'Two exact app references are required.')
    require(isinstance(observations, dict) and isinstance(observations.get('images'), list),
            'Native image observations are required.')
    images = observations['images']
    require(len(images) == len(ROLES) and all(isinstance(x, dict) for x in images),
            'Native image role inventory differs.')
    roles = [x.get('role') for x in images]
    require(all(isinstance(x, str) for x in roles) and set(roles) == ROLES and len(set(roles)) == len(roles),
            'Native image role is missing, unknown or duplicated.')
    expected = dict(previous['images'], **app_references)
    selected = {}
    for image in images:
        role = image['role']
        require(image.get('Os') == 'linux' and image.get('Architecture') == 'amd64' and
                isinstance(image.get('Id'), str) and re.fullmatch(r'sha256:[a-f0-9]{64}', image['Id']),
                'Native image platform or identity differs.')
        digests = image.get('RepoDigests')
        require(isinstance(digests, list) and all(digest_reference(x) for x in digests) and
                digests.count(expected[role]) == 1, 'Expected native RepoDigest is absent or ambiguous.')
        selected[role] = next(x for x in digests if x == expected[role])
    return {'schema_version': SCHEMA, 'images': selected}


def publisher_apps(value):
    require(isinstance(value, dict) and isinstance(value.get('images'), list) and len(value['images']) == 2,
            'Pinned app publisher receipt is incomplete.')
    rows = value['images']
    require(all(isinstance(x, dict) for x in rows) and {x.get('role') for x in rows} == APPS,
            'Pinned app publisher roles differ.')
    selected = {}
    for row in rows:
        repository = 'ghcr.io/foigent/unirouter-' + row['role']
        ref = row.get('registry_digest_reference')
        require(row.get('repository') == repository and digest_reference(ref) and ref.split('@')[0] == repository,
                'Pinned app publisher repository differs.')
        selected[row['role']] = ref
    return selected


def read_public(path, expected=None):
    require(path.suffix != '.dmg', 'Only public JSON metadata is supported.')
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW)
    with os.fdopen(fd, 'rb') as stream:
        info = os.fstat(stream.fileno())
        require(stat.S_ISREG(info.st_mode) and info.st_size <= 4 * 1024 * 1024, 'Public metadata is not a bounded regular file.')
        raw = stream.read()
    require(expected is None or re.fullmatch(r'[a-f0-9]{64}', expected) and hashlib.sha256(raw).hexdigest() == expected,
            'Public metadata SHA-256 differs.')
    return strict_json(raw)


def wire(value):
    return (json.dumps(value, ensure_ascii=False, sort_keys=True, indent=2) + '\n').encode()


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ('previous', 'publisher', 'observations'):
        parser.add_argument('--' + name, type=Path, required=True)
        parser.add_argument('--' + name + '-sha256', required=True)
    actions = parser.add_mutually_exclusive_group(required=True)
    actions.add_argument('--out', type=Path)
    actions.add_argument('--check-descriptor', type=Path)
    args = parser.parse_args(argv)
    try:
        previous = read_public(args.previous, args.previous_sha256)
        apps = publisher_apps(read_public(args.publisher, args.publisher_sha256))
        observations = read_public(args.observations, args.observations_sha256)
        value = select_descriptor(previous, apps, observations)
        raw = wire(value)
        if args.check_descriptor is not None:
            require(read_public(args.check_descriptor) == value, 'Descriptor differs from exact native selected references.')
        else:
            fd = os.open(args.out, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
            with os.fdopen(fd, 'wb') as stream:
                stream.write(raw)
                stream.flush()
                os.fsync(stream.fileno())
        print(json.dumps({'validated': True, 'roles': 6, 'bytes': len(raw),
                          'sha256': hashlib.sha256(raw).hexdigest(), 'Docker_network_signing_or_service_operations': 0}))
        return 0
    except (DescriptorError, OSError, ValueError, TypeError, KeyError):
        print('Descriptor preparation refused: public metadata, exact native identity or output differs.', file=sys.stderr)
        return 1


if __name__ == '__main__':
    sys.exit(main())
