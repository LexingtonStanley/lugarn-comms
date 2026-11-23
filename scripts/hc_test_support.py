"""Private-session-only hc test transport. Never discovers or connects to default."""
import json
import os
from pathlib import Path
import queue
import secrets
import shutil
import subprocess
import threading

# Supplied by Nova's read-only reconnaissance; no default-core probe is needed.
DEFAULT_CORE = '57d257e310b5b777c21d711f6e61f21d'

def clean_env():
    return {k: v for k, v in os.environ.items() if not k.upper().startswith('HC_')}

class Peer:
    def __init__(self, binary, session, recordings=None):
        assert session.startswith('lugarn-test-')
        env = clean_env()
        self.p = subprocess.Popen([binary, 'connect', '--session', session],
                                 stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                 stderr=subprocess.DEVNULL, text=True, env=env)
        self.q = queue.Queue()
        self.notes = []
        self.recordings = recordings
        self.seq = 0
        threading.Thread(target=self._read, daemon=True).start()
        self.hello = self.call('hello', generation=1)
        assert self.hello['core']['id'] != DEFAULT_CORE, 'REFUSING default core'
        assert self.hello['generation'] == 1

    def _read(self):
        for line in self.p.stdout:
            msg = json.loads(line)
            if 'id' in msg:
                self.q.put(msg)
            else:
                self.notes.append(msg)

    def call(self, method, **params):
        self.seq += 1
        request = dict(jsonrpc='2.0', id=self.seq, method=method, params=params)
        self.p.stdin.write(json.dumps(request) + '\n')
        self.p.stdin.flush()
        reply = self.q.get(timeout=90)
        assert reply['id'] == self.seq
        if self.recordings is not None:
            self.recordings.append(dict(request=request, response=reply))
        if 'error' in reply:
            raise RuntimeError(reply['error'])
        return reply['result']

    def close(self):
        self.p.stdin.close()
        try:
            self.p.wait(timeout=5)
        except subprocess.TimeoutExpired:
            self.p.kill()
            self.p.wait()

class PrivateCore:
    def __init__(self):
        self.binary = shutil.which('herdr-core') or str(Path.home()/'.local/bin/herdr-core')
        self.session = 'lugarn-test-' + secrets.token_hex(8)
        self.state = Path(os.environ.get('XDG_STATE_HOME', str(Path.home()/'.local/state')))/'herdr-core'/self.session
        runtime = os.environ.get('XDG_RUNTIME_DIR')
        if not runtime:
            raise RuntimeError('private live tests require XDG_RUNTIME_DIR')
        self.socket = Path(runtime)/'herdr-core'/(self.session+'.sock')
        assert not self.socket.exists() and not self.state.exists(), 'refusing existing session'
        self.recordings = []
        self.peer = None

    def __enter__(self):
        print('private hc session:', self.session, flush=True)
        try:
            self.peer = Peer(self.binary, self.session, self.recordings)
            return self
        except BaseException:
            self.cleanup()
            raise

    def cleanup(self):
        if self.peer:
            self.peer.close()
        env = clean_env()
        env['HC_SESSION'] = self.session
        subprocess.run(['env', '-u', 'HC_TERMINAL_ID', 'HC_SESSION='+self.session,
                        self.binary, 'server', 'stop'], env=env, check=True,
                       capture_output=True, text=True, timeout=30)
        shutil.rmtree(self.state, ignore_errors=True)
        print('removed hc session:', self.session, flush=True)

    def __exit__(self, *_):
        self.cleanup()
