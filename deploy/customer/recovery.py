"""Shared cold-recovery checks, independent of release and host orchestration.

The byte/metadata/archive/SQL algorithms originate in the verified M127 paired
backup helper (sha256 f23c47b740760c246d29657403c3af2f20616410f3482d07b23db0409a78fd31).
Only concrete host paths, content identity and database name become caller
inputs. Schema admission, stop proofs and release authority stay with their
existing native owners. Reuse of these algorithms does not certify a new
schema, release, Target or restore on its own.
"""
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import stat
import tarfile

class RecoveryError(Exception):
    pass

def need(ok, text):
    if not ok:
        raise RecoveryError(text)

def sha(data):
    return hashlib.sha256(data).hexdigest()



def filemeta(p):
    a = p.lstat(); h = hashlib.sha256()
    need(stat.S_ISREG(a.st_mode), 'non-regular private file refused')
    with p.open('rb') as f:
        while b := f.read(1 << 20): h.update(b)
    z = p.lstat()
    need((a.st_dev, a.st_ino, a.st_mtime_ns, a.st_size) == (z.st_dev, z.st_ino, z.st_mtime_ns, z.st_size),
         'file changed while fingerprinting')
    return {'kind': 'file', 'sha256': h.hexdigest(), 'bytes': z.st_size,
            'uid': z.st_uid, 'gid': z.st_gid, 'mode': stat.S_IMODE(z.st_mode)}



def content_inventory(root, objects, identity):
    need(root.is_dir() and not root.is_symlink(), 'body root invalid')
    marker = root / '.unirouter-content-store'
    need(marker.stat().st_size <= 4096, 'oversized body marker')
    binding = json.loads(marker.read_bytes())
    need(binding['identity'] == identity and len(binding['token']) == 64 and
         all(c in '0123456789abcdef' for c in binding['token']), 'body marker identity differs')
    expected = {}
    for row in objects:
        name = row['path']
        need(len(name) == 64 and all(c in '0123456789abcdef' for c in name) and name not in expected and
             row['identity'] == identity and row['binding_sha'] == sha(binding['token'].encode()), 'body row binding differs')
        meta = filemeta(root / name)
        need(meta['bytes'] == row['bytes'] and 'sha256:' + meta['sha256'] == row['digest'], 'body object bytes differ')
        expected[name] = meta
    need({p.name for p in root.iterdir()} == set(expected) | {marker.name}, 'unexplained body entry')
    expected[marker.name] = filemeta(marker)
    return expected



def private_inventory(roots, prefix=Path('/')):
    """Secrets and per-file digests remain exclusively in host-private pair files."""
    prefix = prefix.resolve(strict=True)
    entries = {}
    for original in roots:
        relative = str(original).lstrip('/'); start = prefix / relative
        need(os.path.lexists(start), 'required recovery dependency absent')
        paths = [start]
        if start.is_dir() and not start.is_symlink():
            paths += sorted(start.rglob('*'))
        for p in paths:
            name = p.relative_to(prefix).as_posix(); st = p.lstat()
            need(name not in entries, 'overlapping archive roots')
            common = {'uid': st.st_uid, 'gid': st.st_gid, 'mode': stat.S_IMODE(st.st_mode)}
            if stat.S_ISREG(st.st_mode): meta = filemeta(p)
            elif stat.S_ISDIR(st.st_mode): meta = dict(common, kind='dir')
            elif stat.S_ISLNK(st.st_mode):
                target = os.readlink(p)
                need(not os.path.isabs(target), 'absolute recovery symlink needs explicit resolution before backup')
                meta = dict(common, kind='symlink', target=target)
            else: raise RuntimeError('special recovery file refused')
            entries[name] = meta
    for name, meta in entries.items():
        if meta['kind'] == 'symlink':
            resolved = (prefix / name).resolve(strict=True)
            need(resolved.is_relative_to(prefix.resolve()) and resolved.relative_to(prefix).as_posix() in entries,
                 'recovery symlink target is outside exact archived dependencies')
    return entries



def verify_archive(path, entries, body=False):
    seen = set()
    with tarfile.open(path, 'r:') as archive:
        for member in archive:
            name = member.name.removeprefix('./').rstrip('/')
            if not name or name == '.':
                need(body and member.isdir(), 'unexpected archive root'); continue
            need(not PurePosixPath(name).is_absolute() and '..' not in PurePosixPath(name).parts and
                 name in entries and name not in seen, 'unsafe or duplicate archive member')
            m = entries[name]
            need((member.uid, member.gid, member.mode) == (m['uid'], m['gid'], m['mode']), 'archive ownership/mode differs')
            if m['kind'] == 'file':
                need(member.isfile() and member.size == m['bytes'], 'archive file type/size differs')
                h = hashlib.sha256()
                with archive.extractfile(member) as f:
                    while b := f.read(1 << 20): h.update(b)
                need(h.hexdigest() == m['sha256'], 'archive file hash differs')
            elif m['kind'] == 'dir': need(member.isdir(), 'archive directory differs')
            else: need(member.issym() and member.linkname == m['target'], 'archive symlink differs')
            seen.add(name)
    need(seen == set(entries), 'archive membership incomplete')
    # Prevent extraction through an earlier symlink (normal TLS leaf links pass).
    links = {name for name, m in entries.items() if m['kind'] == 'symlink'}
    need(not any(str(parent) in links for name in entries for parent in PurePosixPath(name).parents),
         'archive contains a symlink ancestor')



def table_fingerprints(sql, db, normalize=False, max_epoch=0, projections=None):
    names = json.loads(sql(db, "SELECT jsonb_agg(tablename ORDER BY tablename) FROM pg_tables WHERE schemaname='public'"))
    parts = []
    for name in names:
        expr, where = (projections or {}).get(name, ('to_jsonb(t)', ''))
        if normalize and name == 'execution_content_streams': expr += "-'recovery_epoch'"
        if normalize and name == 'execution_source_budget_coverage':
            expr += "-'ended_at'"; where = ' WHERE coverage_epoch<=' + str(max_epoch)
        table = '"' + name.replace('"', '""') + '"'; label = "'" + name.replace("'", "''") + "'"
        aggregate = "(SELECT jsonb_build_object('count',count(*),'sha256',encode(sha256(convert_to(COALESCE(string_agg(encode(sha256(convert_to((" + expr + ")::text,'UTF8')),'hex'),'' ORDER BY (" + expr + ")::text),''),'UTF8')),'hex')) FROM " + table + ' t' + where + ')'
        parts.append('SELECT ' + label + '::text AS name,' + aggregate + ' AS value')
    return json.loads(sql(db, "SET TIME ZONE 'UTC'; BEGIN READ ONLY; SELECT jsonb_object_agg(name,value) FROM (" +
                          ' UNION ALL '.join(parts) + ') fingerprints; COMMIT;'))



def boundaries(sql, db):
    return json.loads(sql(db, "SET TIME ZONE 'UTC'; SELECT jsonb_build_object('streams',(SELECT COALESCE(jsonb_agg(to_jsonb(s) ORDER BY org_id,project_id),'[]'::jsonb) FROM execution_content_streams s),'coverage',(SELECT jsonb_agg(to_jsonb(c) ORDER BY coverage_epoch) FROM execution_source_budget_coverage c))"))



def quiet(sql, database):
    q = """BEGIN READ ONLY; WITH latest AS (SELECT DISTINCT ON(org_id,project_id,source_fact_id) document FROM execution_content_manifests ORDER BY org_id,project_id,source_fact_id,revision DESC)
    SELECT jsonb_build_object('clients',(SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid() AND backend_type='client backend'),
    'prepared',(SELECT count(*) FROM pg_prepared_xacts),'unsettled',(SELECT count(*) FROM execution_content_objects WHERE state IN ('reserved','orphan') AND reclaimed_at IS NULL),
    'reserved',(SELECT count(*) FROM execution_content_policies WHERE reserved_bytes<>0),'expired_ready',(SELECT count(*) FROM execution_content_objects WHERE state='ready' AND expires_at<=clock_timestamp()),
    'pending_capture',(SELECT count(*) FROM latest WHERE convert_from(document,'UTF8')::jsonb#>>'{capture,completeness}'='pending'),
    'active_generations',(SELECT count(*) FROM runtime_generations WHERE state<>'stopped' OR ingress_state<>'absent'),
    'pending_ingress',(SELECT count(*) FROM runtime_ingress_changes WHERE status<>'applied')); COMMIT;"""
    result = json.loads(sql(database, q)); need(not any(result.values()), 'final quiescence predicates not all zero')
    return result
