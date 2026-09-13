#!/usr/bin/env python3
"""Exercise the shipped shell body against an isolated file, never /proc."""
from pathlib import Path
import os
import subprocess
import tempfile
import yaml

chart = Path(__file__).resolve().parents[1]
values = yaml.safe_load((chart / 'values.yaml').read_text())
init = values['gha-runner-scale-set']['template']['spec']['initContainers'][0]
assert init['name'] == 'configure-inotify'
assert init['securityContext']['runAsUser'] == 0 and init['securityContext']['privileged'] is True
source = init['command'][2]
assert source.count('/proc/sys/fs/inotify/max_user_instances') == 1
assert 'max_user_watches' not in source
for name, before, requested, expected, success in [
    ('default raises the observed ARC ceiling', '128', None, '1024', True),
    ('an existing higher ceiling is preserved', '4096', '1024', '4096', True),
    ('the configured floor is effective', '128', '2048', '2048', True),
    ('zero explicitly disables node tuning', '128', '0', '128', True),
    ('bad configuration refuses runner startup', '128', 'bad', '128', False),
    ('leading zero decimal input is normalized', '128', '0002048', '2048', True),
    ('overflow refuses runner startup', '128', '99999999999999999999999', '128', False),
    ('kernel integer overflow refuses runner startup', '128', '2147483648', '128', False),
    ('negative configuration refuses runner startup', '128', '-1', '128', False),
]:
    with tempfile.TemporaryDirectory(prefix='arc-inotify-') as tmp:
        target = Path(tmp) / 'limit'
        target.write_text(before + '\n')
        script = source.replace('/proc/sys/fs/inotify/max_user_instances', str(target))
        env = {k: v for k, v in os.environ.items() if k != 'ARC_INOTIFY_MIN_USER_INSTANCES'}
        if requested is not None:
            env['ARC_INOTIFY_MIN_USER_INSTANCES'] = requested
        result = subprocess.run(['/bin/sh', '-ec', script], env=env, capture_output=True, text=True, timeout=5)
        assert (result.returncode == 0) == success, (name, result.stderr)
        assert target.read_text().strip() == expected, (name, target.read_text())
        print('PASS:', name)

# An unreadable/missing sysctl fails the real shell body before runner startup.
with tempfile.TemporaryDirectory(prefix='arc-inotify-') as tmp:
    missing = Path(tmp) / 'missing'
    script = source.replace('/proc/sys/fs/inotify/max_user_instances', str(missing))
    env = dict(os.environ, ARC_INOTIFY_MIN_USER_INSTANCES='1024')
    result = subprocess.run(['/bin/sh', '-ec', script], env=env, capture_output=True, text=True, timeout=5)
    assert result.returncode != 0 and not missing.exists(), result
    print('PASS: a missing sysctl refuses runner startup')

# Validate the effective PodSpec after the pinned ARC subchart has rendered it.
rendered = subprocess.check_output(['helm', 'template', 'arc-runners', str(chart), '--namespace', 'arc-runners'], text=True)
ars = next(d for d in yaml.safe_load_all(rendered) if d and d['kind'] == 'AutoscalingRunnerSet')
spec = ars['spec']['template']['spec']
assert spec['initContainers'][0] == init
assert [c['name'] for c in spec['initContainers']] == ['configure-inotify', 'init-dind-externals']
assert {c['name'] for c in spec['containers']} == {'runner', 'dind'}
print('PASS: rendered runner PodSpec keeps the initializer before all work')
