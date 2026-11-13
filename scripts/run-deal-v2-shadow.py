#!/usr/bin/env python3
"""Build and run the fixed phase-3 shadow experiment on the authorized test host."""
import argparse
import json
import os
from pathlib import Path
import shlex
import subprocess
import tarfile
import tempfile

ROOT = Path(__file__).resolve().parents[1]
REMOTE = "/opt/lingce-test/experiments/deal-v2-phase3-20261031"
TARGET = "root@47.79.19.8"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--rules", default="deal-compiler-v2.0.0", choices=["deal-compiler-v2.0.0", "deal-compiler-v2.1.0-rc7", "deal-compiler-v2.1.0-rc8", "deal-compiler-v2.1.0-rc9", "deal-compiler-v2.1.0-rc10",
        "deal-compiler-v2.1.0-rc11", "deal-compiler-v2.1.0-rc12", "deal-compiler-v2.1.0-rc12.1",
    ])
    parser.add_argument("--prompt", default="", choices=["", "rc11", "rc11.1"], help="explicit prompt package for rules rc11")
    parser.add_argument("--synthetic", action="store_true")
    parser.add_argument("--ids", default="", help="comma separated analysis ids for an explicit small batch")
    parser.add_argument("--execute", action="store_true")
    parser.add_argument("--limit", type=int, default=0)
    parser.add_argument("--batch", default="phase3-current-v2-20261031-02")
    args = parser.parse_args()
    args.output.mkdir(parents=True, exist_ok=True)
    key = str(Path.home() / ".ssh/lingce_test_ed25519")
    ssh = ["ssh", "-o", "BatchMode=yes", "-o", "ConnectTimeout=10", "-i", key, TARGET]
    env = dict(os.environ, GOOS="linux", GOARCH="amd64", CGO_ENABLED="0", GOPROXY="off", GOCACHE="/private/tmp/lingce-gocache")
    with tempfile.TemporaryDirectory(prefix="deal-v2-shadow-build-") as temporary:
        temporary = Path(temporary)
        binary = temporary / "deal-v2-shadow"
        subprocess.run(["go", "build", "-o", str(binary), "./cmd/deal-v2-shadow"], cwd=ROOT, env=env, check=True)
        archive = temporary / "shadow.tar.gz"
        with tarfile.open(archive, "w:gz") as tar:
            tar.add(binary, arcname="deal-v2-shadow")
            tar.add(ROOT / "internal/store/migrations/20261031_073_deal_expert_v2_audit.sql", arcname="audit.sql")
        subprocess.run(ssh + ["mkdir -p " + shlex.quote(REMOTE)], check=True)
        subprocess.run(["scp", "-q", "-i", key, str(archive), TARGET + ":" + REMOTE + "/shadow.tar.gz"], check=True)
    command = "cd " + shlex.quote(REMOTE) + " && tar -xzf shadow.tar.gz && ./deal-v2-shadow --output " + shlex.quote("output/"+args.batch) + " --batch " + shlex.quote(args.batch)
    command += " --rules " + shlex.quote(args.rules)
    if args.prompt:
        command += " --prompt " + shlex.quote(args.prompt)
    if args.synthetic:
        command += " --synthetic"
    if args.ids:
        command += " --ids " + shlex.quote(args.ids)
    if args.execute:
        command += " --execute --limit " + str(args.limit)
    result = subprocess.run(ssh + [command], text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
    name = "execute" if args.execute else "prepare"
    (args.output / ("shadow-" + name + ".txt")).write_text(result.stdout)
    (args.output / ("shadow-" + name + "-command.json")).write_text(json.dumps({"target": TARGET, "directory": REMOTE, "execute": args.execute, "limit": args.limit, "ids": args.ids, "exit_code": result.returncode}, indent=2) + "\n")
    print(result.stdout, end="")
    # Private evidence packets include frozen transcripts and model responses.
    # Keep the whole directory private; never commit real customer output.
    subprocess.run(["scp", "-q", "-r", "-i", key, TARGET + ":" + REMOTE + "/output/"+args.batch, str(args.output)], check=False)
    raise SystemExit(result.returncode)


if __name__ == "__main__":
    main()
