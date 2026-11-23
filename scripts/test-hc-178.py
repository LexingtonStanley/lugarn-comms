#!/usr/bin/env python3
"""Opt-in hc #178 regression. Builds the broker; uses only a fresh private core.
Run: python3 scripts/test-hc-178.py --run
"""
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import time
from hc_test_support import PrivateCore, Peer, clean_env

ROOT=Path(__file__).resolve().parent.parent
FIXTURES=ROOT/'internal/broker/testdata/hc-178'

def save(name,data):
    FIXTURES.mkdir(parents=True,exist_ok=True)
    (FIXTURES/name).write_text(json.dumps(data,indent=2)+'\n')

def until(label,fn):
    deadline=time.monotonic()+55
    while time.monotonic()<deadline:
        result=fn()
        if result:return result
        time.sleep(.2)
    raise AssertionError(label+' timed out')

def main():
    if sys.argv[1:]!=['--run']:sys.exit(__doc__)
    with tempfile.TemporaryDirectory(prefix='lugarn-hc-178-') as tmp, PrivateCore() as core:
        tmp=Path(tmp)
        for part in ['home','state','work','bin']:(tmp/part).mkdir()
        binary=tmp/'lugarn-comms'
        subprocess.run(['go','build','-o',str(binary),'./cmd/lugarn-comms'],cwd=ROOT,check=True)
        helper=tmp/'bin/pi'
        helper.write_text("#!/usr/bin/python3\nimport os,sys,time\nif sys.argv[1:]==['--version']:\n print('pi 0.63.0');sys.exit(0)\ndef report(state):\n print('\\033]7501;state='+state+':app=pi\\007',end='',flush=True)\nreport('idle')\nprint('hc plain harness ready HOME='+os.environ['HOME']+' PATH='+os.environ['PATH'],flush=True)\nfor line in sys.stdin:\n print(line,end='',flush=True)\n if 'CHECK178' in line:\n  report('working');time.sleep(1);report('idle')\n")
        helper.chmod(0o700)
        env=clean_env()
        env.update(HOME=str(tmp/'home'),SHELL='/bin/sh',
                   PATH=str(tmp/'bin')+os.pathsep+os.environ['PATH'],
                   LUGARN_COMMS_HOME=str(tmp/'state'),LUGARN_COMMS_POINTER_HOME=str(tmp/'home'),
                   LUGARN_COMMS_HC_BINARY=core.binary)
        def app(op,**args):
            result=subprocess.run([str(binary),'app',op],env=env,input=json.dumps(dict(protocol=1,op=op,**args)),text=True,capture_output=True,timeout=90,check=False)
            response=json.loads(result.stdout)
            assert response.get('ok'),dict(response=response,stderr=result.stderr)
            return response['result']
        def terminals():return core.peer.call('terminal.list',all=True)['terminals']
        def members(team):return app('members',team=team)['members']
        def running(team):
            agents=[m for m in members(team) if m['kind']=='agent']
            if any(m['state']=='stopped' and m['stop_reason']=='launch_failed' for m in agents):
                raise AssertionError(dict(members=agents,terminals=terminals(),timeline=app('timeline',team=team)))
            return agents if all(m['state']=='running' and m['activity']=='idle' for m in agents) else None
        def spec(team,placement='tabs',names=('writer',)):
            return dict(version=1,id=team,name=team,display_name='Team '+team.upper(),pattern='custom',herdr=dict(server=core.session,backend='hc',placement=placement),members=[dict(id=team+'-'+n,name=n,role='custom',harness='pi',folder=str(tmp/'work'),profile='workspace',mode='pane',brief='Test shell',task=None,host=None) for n in names],grid=[])
        notes=Peer(core.binary,core.session)
        try:
            save('subscribe.json',notes.call('events.subscribe'))
            for team,placement in [('tabs','tabs'),('spaces','workspaces')]:
                app('apply-team',spec=spec(team,placement,names=('writer','t7')),if_revision=0)
                save(team+'-start.json',app('start-team',team=team))
                ms=until('members '+team,lambda:running(team))
                save(team+'-members.json',ms)
                ts=[t for t in terminals() if t['labels'].get('lugarn.team')==team]
                save(team+'-terminals.json',ts)
                runner=next(t for t in ts if t['labels']['lugarn.role']=='runner')
                assert Path(runner['cwd']).name=='Team '+team.upper()
                assert runner['name'] is None and runner['parent'] is None and runner['keep'] is False
                assert all(t['parent'] is None and t['labels'].get('lugarn.home') for t in ts)
                assert all(len(t['labels']['herdr.space'])==34 and len(t['labels']['herdr.tab'])==34 for t in ts)
                memberts=[t for t in ts if t['labels']['lugarn.role']=='member']
                assert all(t['name']!='t7' for t in memberts)
                assert (len({t['labels']['herdr.space'] for t in ts})==1)==(placement=='tabs')
                text=core.peer.call('terminal.read',terminal=memberts[0]['address'],lines=10)['text']
                assert 'HOME='+str(tmp/'home') in text and str(tmp/'bin') in text,text
            message=app('send',team='tabs',to='tabs-writer',text='CHECK178 delivery')
            save('message.json',message)
            save('message-timeline.json',until('message delivered',lambda: next((e for e in app('timeline',team='tabs')['events'] if e['type']=='message.created' and e['data']['message']['text']=='CHECK178 delivery' and e['data']['message']['deliveries'][0]['state']=='delivered'),None)))
            space='sp'+'a'*32
            external=[]
            for n in ['one','two']:
                external.append(core.peer.call('terminal.run',argv=[str(helper)],cwd=str(tmp/'work'),env={k:env[k] for k in ["HOME","PATH"]},name=n,no_parent=True,no_notify=True,keep=False,labels={'herdr.space':space,'herdr.tab':'tb'+('b' if n=='one' else 'c')*32}))
            until('external status',lambda: all(core.peer.call('terminal.get',terminal=t['address']).get('foreground') for t in external))
            before=[core.peer.call('terminal.get',terminal=t['address']) for t in external]
            adopted=spec('adopted',names=('one','two'))
            save('adopt-before.json',before)
            save('adopt.json',app('adopt-team',spec=adopted,backend='hc',workspace=space,members=[dict(member='adopted-'+n,pane=t['address'],agent=n) for n,t in zip(['one','two'],external)]))
            after=[core.peer.call('terminal.get',terminal=t['address']) for t in external]
            assert before==after,'adoption changed terminals'
            app('start-team',team='adopted')
            runner=next(t for t in terminals() if t['labels'].get('lugarn.team')=='adopted')
            assert not any(k.startswith('herdr.') for k in runner['labels'])
            save('bring-preview.json',app('bring-onto-comms',team='adopted',member='adopted-one'))
            m=next(m for m in members('adopted') if m['name']=='one')
            app('bring-onto-comms',team='adopted',member='adopted-one',confirm=True,if_generation=m['lease']['generation'],expected_session=None)
            brought=until('conversion',lambda: next((m for m in members('adopted') if m['name']=='one' and m['state']=='running'),None))
            save('brought.json',brought)
            new=core.peer.call('terminal.get',terminal=brought['lease']['pane'])
            assert new['labels']['herdr.tab']==external[0]['labels']['herdr.tab']
            assert core.peer.call('terminal.get',terminal=external[0]['address'])['state']=='finished'
            save('forget.json',app('forget-team',team='adopted',if_revision=1))
            assert core.peer.call('terminal.get',terminal=new['address'])['state']=='live'
            assert core.peer.call('terminal.get',terminal=external[1]['address'])['state']=='live'
            for team in ['tabs','spaces']: app('stop-team',team=team)
            assert not any(t['labels'].get('lugarn.team') in ['tabs','spaces'] for t in terminals())
            until('heartbeat',lambda: any(n.get('method')=='heartbeat' or n.get('params',{}).get('type')=='heartbeat' for n in notes.notes))
            save('events.json',notes.notes)
            save('rpc.json',core.recordings)
            print('PASS: placements, names, cwd, environment, status, send, adoption, conversion, forget, stop',flush=True)
        finally:
            save('events.json',notes.notes)
            save('rpc.json',core.recordings)
            notes.close()

if __name__=='__main__':main()
