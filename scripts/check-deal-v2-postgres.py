#!/usr/bin/env python3
"""Run only phase-3 PostgreSQL tests on the authorized test host, without secrets in output."""
import argparse
import io
import json
import os
from pathlib import Path
import shlex
import subprocess
import tarfile
import tempfile
import time

ROOT = Path(__file__).resolve().parents[1]
REMOTE_RUNNER = '''\
import os, pathlib, subprocess, sys, tomllib, urllib.parse
base=pathlib.Path(__file__).resolve().parent
cfg=tomllib.loads(pathlib.Path('/opt/lingce-test/configs/api.toml').read_text())
db=cfg['database']
assert db['name']=='lingce_test' and db['host']=='127.0.0.1' and db['port']==5433, 'unexpected database target'
secrets=tomllib.loads(pathlib.Path(cfg['app']['secrets_file']).read_text())
env=os.environ.copy()
env['DEAL_PHASE3_DATABASE_URL']='postgresql://'+urllib.parse.quote(db['user'],safe='')+':'+urllib.parse.quote(secrets['database']['password'],safe='')+'@127.0.0.1:5433/lingce_test?sslmode=disable'
print('Target: 47.79.19.8 / local PostgreSQL 16 / 127.0.0.1:5433 / lingce_test',flush=True)
r=subprocess.run([str(base/'pipeline.test'),'-test.run','^TestPhase3Postgres','-test.v','-test.count=1','-test.timeout=180s'],env=env,cwd=base/'internal/dealexpert/pipeline',stdout=subprocess.PIPE,stderr=subprocess.STDOUT)
sys.stdout.buffer.write(r.stdout)
print('exit_code='+str(r.returncode),flush=True)
sys.exit(r.returncode)
'''


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--key', type=Path, default=Path.home()/'.ssh/lingce_test_ed25519')
    args = parser.parse_args()
    args.output.mkdir(parents=True, exist_ok=True)
    env = os.environ.copy()
    env.update(GOOS='linux', GOARCH='amd64', CGO_ENABLED='0', GOPROXY='off', GOCACHE='/private/tmp/lingce-gocache')
    target = 'root@47.79.19.8'
    ssh = ['ssh', '-o', 'BatchMode=yes', '-o', 'ConnectTimeout=10', '-i', str(args.key), target]
    remote = '/tmp/deal-v2-postgres-' + str(time.time_ns())
    with tempfile.TemporaryDirectory(prefix='deal-v2-postgres-') as tmp:
        tmp = Path(tmp)
        subprocess.run(['go', 'test', '-c', './internal/dealexpert/pipeline', '-o', str(tmp/'pipeline.test')], cwd=ROOT, env=env, check=True)
        archive = tmp/'test.tar.gz'
        with tarfile.open(archive, 'w:gz', format=tarfile.USTAR_FORMAT) as tar:
            tar.add(tmp/'pipeline.test', arcname='pipeline.test')
            for rel in ['internal/store/migrations/20261031_073_deal_expert_v2_audit.sql', 'internal/dealexpert/compiler/testdata']:
                tar.add(ROOT/rel, arcname=rel)
            for name, content in [('run.py', REMOTE_RUNNER)]:
                raw = content.encode()
                info = tarfile.TarInfo(name)
                info.size, info.mode = len(raw), 0o600
                tar.addfile(info, io.BytesIO(raw))
        result = None
        try:
            subprocess.run(ssh + ['mkdir -p ' + shlex.quote(remote) + '/internal/dealexpert/pipeline'], check=True)
            subprocess.run(['scp', '-q', '-i', str(args.key), str(archive), target+':'+remote+'/test.tar.gz'], check=True)
            command = 'cd '+shlex.quote(remote)+' && tar -xzf test.tar.gz && python3 run.py'
            result = subprocess.run(ssh+[command], stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True)
            (args.output/'postgres-store-final.txt').write_text(result.stdout)
            print(result.stdout, end='')
            (args.output/'postgres-command.json').write_text(json.dumps({'command':'python3 scripts/check-deal-v2-postgres.py --output <report-directory>', 'target':'47.79.19.8:5433/lingce_test', 'exit_code':result.returncode}, indent=2)+'\n')
        finally:
            # Only the unique directory created by this invocation is removed.
            subprocess.run(ssh+['rm -rf -- '+shlex.quote(remote)], check=True)
        if result is None or result.returncode:
            raise SystemExit(result.returncode if result else 1)


if __name__ == '__main__':
    main()
