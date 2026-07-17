#!/usr/bin/env python3
"""Retired: customer releases use the signed deploy/customer artifacts."""

import re
import sys
import os
import argparse


print(
    "ERROR: fix-compose-for-deploy.py is retired and must not rewrite source Compose. "
    "Use the signed deploy/customer package and docs/ops/deployment-runbook.md.",
    file=sys.stderr,
)
sys.exit(2)


def remove_yaml_block(lines, start_idx, indent_level):
    """从 start_idx 开始，移除指定缩进级别下的整个 YAML 块。
    返回移除后的行列表。"""
    result = lines[:start_idx]
    i = start_idx + 1
    while i < len(lines):
        line = lines[i]
        stripped = line.lstrip()
        if stripped == '' or stripped.startswith('#'):
            i += 1
            continue
        current_indent = len(line) - len(stripped)
        if current_indent <= indent_level:
            break
        i += 1
    result.extend(lines[i:])
    return result


def fix_compose_file(filepath, service_name):
    """修复单个 compose 文件中的指定服务。"""
    if not os.path.exists(filepath):
        print(f"SKIP: {filepath} not found")
        return False

    with open(filepath, 'r') as f:
        lines = f.readlines()

    # 找到 service 定义
    svc_pattern = re.compile(rf'^(\s+){service_name}:\s*$')
    svc_indent = None
    svc_start = None
    for i, line in enumerate(lines):
        m = svc_pattern.match(line)
        if m:
            svc_indent = len(m.group(1))
            svc_start = i
            break

    if svc_start is None:
        print(f"SKIP: service '{service_name}' not found in {filepath}")
        return False

    # 在 service 块内找 build: 块
    build_indent = svc_indent + 2
    build_start = None
    for i in range(svc_start + 1, len(lines)):
        line = lines[i]
        stripped = line.lstrip()
        if stripped == '':
            continue
        current_indent = len(line) - len(stripped)
        if current_indent <= svc_indent and stripped != '' and not stripped.startswith('#'):
            break
        if current_indent == build_indent and stripped.startswith('build:'):
            build_start = i
            break

    if build_start is None:
        print(f"SKIP: build: not found for '{service_name}'")
        return False

    # 在 build 块内找 dockerfile: 和 args:
    dockerfile_indent = build_indent + 2
    changes = 0
    args_idx = None
    i = build_start + 1
    while i < len(lines):
        line = lines[i]
        stripped = line.lstrip()
        if stripped == '':
            i += 1
            continue
        current_indent = len(line) - len(stripped)
        if current_indent <= build_indent:
            break
        if current_indent == dockerfile_indent:
            if 'dockerfile: Dockerfile' in stripped and 'Dockerfile.deploy' not in stripped:
                lines[i] = line.replace('dockerfile: Dockerfile', 'dockerfile: Dockerfile.deploy')
                print(f"  ✓ Changed {service_name}.build.dockerfile → Dockerfile.deploy")
                changes += 1
            elif stripped.startswith('args:'):
                args_idx = i
        i += 1

    # 移除 args 块
    if args_idx is not None:
        lines = remove_yaml_block(lines, args_idx, dockerfile_indent)
        print(f"  ✓ Removed {service_name}.build.args block")
        changes += 1

    if changes == 0:
        print(f"  ✓ Already in deploy mode (no changes needed)")
        return False

    with open(filepath, 'w') as f:
        f.writelines(lines)

    print(f"  ✓ {filepath} updated ({changes} changes)")
    return True


def validate_compose(filepath):
    """验证修改后的 compose 文件。"""
    with open(filepath, 'r') as f:
        content = f.read()

    issues = []

    # 检查 backend 使用 Dockerfile.deploy
    in_backend = False
    in_frontend = False
    for line in content.split('\n'):
        stripped = line.strip()
        if stripped.startswith('backend:') and not stripped.startswith('backend-canary:'):
            in_backend = True
            in_frontend = False
        elif stripped.startswith('frontend:'):
            in_frontend = True
            in_backend = False
        elif stripped and not stripped.startswith('#') and not line.startswith(' '):
            in_backend = False
            in_frontend = False

        if in_backend and 'dockerfile: Dockerfile' in stripped and 'Dockerfile.deploy' not in stripped:
            issues.append(f"  ✗ backend: dockerfile should be Dockerfile.deploy")
        if in_frontend and 'dockerfile: Dockerfile.deploy' in stripped:
            issues.append(f"  ✗ frontend: dockerfile should NOT be Dockerfile.deploy")
        if in_backend and 'args:' in stripped:
            issues.append(f"  ✗ backend: args should be removed")

    if issues:
        for issue in issues:
            print(issue)
        return False
    print(f"  ✓ Validation passed")
    return True


def main():
    parser = argparse.ArgumentParser(description='Fix docker-compose for deployment')
    parser.add_argument('--canary', action='store_true', help='Also fix docker-compose.canary.yml')
    parser.add_argument('--dir', default='/opt/unirouter', help='Project directory')
    parser.add_argument('--validate-only', action='store_true', help='Only validate, do not modify')
    args = parser.parse_args()

    compose_dir = args.dir
    main_file = os.path.join(compose_dir, 'docker-compose.yml')
    canary_file = os.path.join(compose_dir, 'docker-compose.canary.yml')

    print("=== Fix compose for deploy ===\n")

    if args.validate_only:
        print(f"Validating {main_file}:")
        validate_compose(main_file)
        if args.canary:
            print(f"\nValidating {canary_file}:")
            validate_compose(canary_file)
        return

    # 修复主 compose
    print(f"Fixing {main_file}:")
    fix_compose_file(main_file, 'backend')
    validate_compose(main_file)

    # 修复 canary compose
    if args.canary:
        print(f"\nFixing {canary_file}:")
        fix_compose_file(canary_file, 'backend-canary')
        validate_compose(canary_file)

    print("\nDone.")


if __name__ == '__main__':
    main()
