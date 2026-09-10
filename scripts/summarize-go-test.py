#!/usr/bin/env python3
"""Summarize one go test -json log without printing test payloads or credentials."""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess


def summarize(path, exit_code):
    packages, tests, locations = {}, {}, []
    started = set()
    with path.open(errors="replace") as stream:
        for line_number, line in enumerate(stream, 1):
            try:
                event = json.loads(line)
            except json.JSONDecodeError:
                continue  # Compiler diagnostics stay in the original log.
            if not isinstance(event, dict):
                continue
            package, test, action = event.get("Package"), event.get("Test"), event.get("Action")
            if not package:
                continue
            if action == "start" and not test:
                started.add(package)
            if action in ("pass", "fail", "skip"):
                if test:
                    tests[(package, test)] = action
                else:
                    packages[package] = action
                if action == "fail":
                    locations.append({"line": line_number, "package": package, "test": test})
    count = lambda values: {state: sum(v == state for v in values) for state in ("pass", "fail", "skip")}
    incomplete = sorted(started - packages.keys())
    passed = exit_code == 0 and "pass" in tests.values() and "fail" not in tests.values() and "fail" not in packages.values() and not incomplete
    return {"status": "PASS" if passed else "FAIL", "exit_code": exit_code,
            "packages": count(packages.values()), "tests_including_subtests": count(tests.values()),
            "incomplete_packages": incomplete, "failure_locations": locations,
            "log": str(path.resolve())}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("log", type=Path)
    parser.add_argument("--exit-code", required=True, type=int)
    parser.add_argument("--group", required=True)
    args = parser.parse_args()
    result = summarize(args.log, args.exit_code)
    repo = Path(__file__).resolve().parent.parent
    def git(*arguments):
        return subprocess.check_output(["git", *arguments], cwd=repo)
    diff = git("diff", "--binary", "HEAD")
    result.update(group=args.group, source={
        "commit": git("rev-parse", "HEAD").decode().strip(),
        "tree": git("rev-parse", "HEAD^{tree}").decode().strip(),
        "tracked_diff_sha256": hashlib.sha256(diff).hexdigest(),
        "untracked_paths": git("ls-files", "--others", "--exclude-standard").decode().splitlines(),
        "runner_sha256": hashlib.sha256((repo / "scripts/test-backend-integration.sh").read_bytes()).hexdigest(),
        "reuse": "Requires assessment of candidate, fixture, dependencies and affected behavior; never automatic."})
    target = args.log.with_suffix(".summary.json")
    with target.open("x") as output:
        json.dump(result, output, indent=2)
        output.write("\n")
    print(json.dumps({key: result[key] for key in ("group", "status", "exit_code", "packages", "tests_including_subtests")}, separators=(",", ":")))
    print(f"evidence={target} log={args.log}")
    for failure in result["failure_locations"][:5]:
        print(f"failure={args.log}:{failure['line']} {failure['package']} {failure['test'] or ''}")
    return 0 if result["status"] == "PASS" else 1


if __name__ == "__main__":
    raise SystemExit(main())
