#!/usr/bin/env python3
"""Record real hc error, heartbeat, missed and harness-status fixtures privately."""
import json
import os
from pathlib import Path
import queue
import shutil
import subprocess
import sys
import tempfile
import threading
import time
from hc_test_support import PrivateCore, Peer, clean_env

ROOT=Path(__file__).resolve().parent.parent
OUT=ROOT/'internal/hc/testdata'

def save(name,data):
    (OUT/name).write_text(json.dumps(data,indent=2)+'\n')

def main():
    if sys.argv[1:]!=['--run']:sys.exit(__doc__)
    with PrivateCore() as core,tempfile.TemporaryDirectory(prefix='hc-probe-178-') as tmp:
        tmp=Path(tmp)
        subscriber=subprocess.Popen([core.binary,'connect','--session',core.session],env=clean_env(),stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=subprocess.DEVNULL,text=True)
        try:
            def call(i,method,params):
                subscriber.stdin.write(json.dumps(dict(jsonrpc='2.0',id=i,method=method,params=params))+'\n');subscriber.stdin.flush()
                return json.loads(subscriber.stdout.readline())
            hello=call(1,'hello',{'generation':1})
            assert hello['result']['core']['id']==core.peer.hello['core']['id']
            save('hello.json',hello['result'])
            save('subscribe.json',call(2,'events.subscribe',{'labels':{'probe':'missed'}})['result'])
            # Stop draining the subscriber while this private terminal emits a burst.
            burst="import sys,time;time.sleep(1);sys.stdout.write('\\x1b]7501;state=working:app=fixture\\x07\\x1b]7501;state=idle:app=fixture\\x07'*50000);sys.stdout.flush();time.sleep(3)"
            t=core.peer.call('terminal.run',argv=[sys.executable,'-c',burst],cwd=str(tmp),labels={'probe':'missed'},no_parent=True,no_notify=True,keep=False)
            save('terminal.json',t)
            time.sleep(5)
            q=queue.Queue()
            def read():
                for line in subscriber.stdout:q.put(json.loads(line))
            threading.Thread(target=read,daemon=True).start()
            selected=[];deadline=time.monotonic()+40
            while time.monotonic()<deadline:
                try:n=q.get(timeout=1)
                except queue.Empty:continue
                if n['method'] in ['missed','heartbeat']:
                    selected.append(n)
                if all(any(n['method']==kind for n in selected) for kind in ['missed','heartbeat']):break
            save('recovery-events.json',selected)
            assert any(n['method']=='missed' for n in selected),'no real missed event captured'
            assert any(n['method']=='heartbeat' for n in selected),'no heartbeat captured'
            print('recorded missed and heartbeat',flush=True)
        finally:
            subscriber.stdin.close();subscriber.terminate();subscriber.wait(timeout=5)
        probe=core.peer.call('terminal.run',argv=['/bin/cat'],cwd=str(tmp),name='error-probe',no_parent=True,no_notify=True,keep=False)
        for method,params in [('terminal.run',dict(argv=['/bin/cat'],name='error-probe')),('terminal.run',dict(argv=['/bin/cat'],name='t7'))]:
            try:core.peer.call(method,**params)
            except RuntimeError:pass
            else:raise AssertionError('expected hc refusal')
        core.peer.call('terminal.kill',terminal=probe['address'])
        try:core.peer.call('terminal.send',terminal=probe['address'],text='finished')
        except RuntimeError:pass
        else:raise AssertionError('send to finished accepted')
        core.peer.call('terminal.rm',terminal=probe['address'])
        try:core.peer.call('terminal.get',terminal=probe['address'])
        except RuntimeError:pass
        else:raise AssertionError('removed terminal exists')
        results={}
        for name,extra in [('opencode',{'OPENCODE_DISABLE_AUTOUPDATE':'1','OPENCODE_CONFIG_CONTENT':'{}'}),('pi',{'PI_SKIP_VERSION_CHECK':'1','PI_CODING_AGENT_DIR':str(tmp/'pi')})]:
            binary=shutil.which(name)
            if not binary:
                results[name]={'installed':False};continue
            home=tmp/name;home.mkdir()
            env=dict(HOME=str(home),XDG_CONFIG_HOME=str(home/'config'),XDG_DATA_HOME=str(home/'data'),XDG_CACHE_HOME=str(home/'cache'),**extra)
            version=subprocess.run([binary,'--version'],env=dict(clean_env(),**env),capture_output=True,text=True,timeout=10).stdout.strip()
            t=core.peer.call('terminal.run',argv=[binary],cwd=str(tmp),env=env,no_parent=True,no_notify=True,keep=False)
            deadline=time.monotonic()+15
            while time.monotonic()<deadline:
                t=core.peer.call('terminal.get',terminal=t['address'])
                if t.get('reported') or t['state']=='finished':break
                time.sleep(.5)
            results[name]={'version':version,'installed':True,'reported':t.get('reported'),'status':t.get('status'),'state':t['state'],'foreground':t.get('foreground')}
            if t['state']=='live':core.peer.call('terminal.kill',terminal=t['address'])
            core.peer.call('terminal.rm',terminal=t['address'])
        save('harness-status.json',results)
        save('probe-rpc.json',core.recordings)
        print('harness status:',json.dumps(results),flush=True)

if __name__=='__main__':main()
