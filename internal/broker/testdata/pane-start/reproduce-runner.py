import os,sys,subprocess,pathlib,tempfile,secrets,json,time,shutil
binary,label=sys.argv[1:]
home=tempfile.mkdtemp(prefix='lp-'); session='lugarn-test-'+secrets.token_hex(6)
env={k:v for k,v in os.environ.items() if not k.upper().startswith('HERDR_')}
env.update(HOME=home,SHELL='/usr/bin/zsh',ZDOTDIR=home,XDG_CONFIG_HOME=home+'/.config',LUGARN_COMMS_HOME=home+'/state',LUGARN_COMMS_POINTER_HOME=home)
bin=pathlib.Path(home+'/bin');bin.mkdir();fake=bin/'codex';fake.write_text('#!/bin/sh\nif [ "$1" = "--version" ]; then echo "codex-cli 0.164.0"; exit; fi\necho "Fake harness refuses every non-version invocation" >&2\nexit 97\n');fake.chmod(0o700)
env['PATH']=str(bin)+':'+env['PATH']
pathlib.Path(home+'/.zshrc').write_text('print -r -- "STARTUP_QUESTION: answer ready"\nread -r answer\nprint -r -- "STARTUP_ANSWER=$answer"\nPS1="SHELL_READY> "\n')
conf=pathlib.Path(home+'/.config/herdr');conf.mkdir(parents=True);(conf/'config.toml').write_text('[terminal]\ndefault_shell = "/usr/bin/zsh"\nshell_mode = "non_login"\n')
def cmd(argv,input=None):
 r=subprocess.run(argv,env=env,input=json.dumps(input) if input else None,text=True,capture_output=True,timeout=45)
 print('$ '+' '.join(argv),flush=True);print(r.stdout+r.stderr,end='',flush=True);print('exit='+str(r.returncode),flush=True);return r
def herdr(*args): return cmd(['herdr','--session',session,*args])
server=None
try:
 print(label+' HOME='+home+' SHELL='+env['SHELL']+' session='+session,flush=True)
 print('.zshrc:\n'+pathlib.Path(home+'/.zshrc').read_text(),flush=True)
 # The server is ours, and its initial environment and configuration are private.
 log=open(home+'/server.log','w');server=subprocess.Popen(['herdr','--session',session,'server'],env=env,stdin=subprocess.DEVNULL,stdout=log,stderr=subprocess.STDOUT,start_new_session=True)
 time.sleep(.6)
 herdr('status','--json')
 # Baseline includes a direct pane test so the swallowed command is visible.
 ws=json.loads(herdr('workspace','create','--cwd',home,'--label','startup-evidence','--no-focus').stdout)['result'];pane=ws['root_pane']['pane_id']
 time.sleep(.3)
 herdr('pane','read',pane,'--source','recent-unwrapped','--lines','10')
 herdr('pane','process-info','--pane',pane)
 herdr('pane','run',pane,'/usr/bin/touch '+home+'/runner-was-executed')
 time.sleep(.3)
 herdr('pane','read',pane,'--source','recent-unwrapped','--lines','12')
 print('typed command executed = '+str(pathlib.Path(home+'/runner-was-executed').exists()),flush=True)
 herdr('pane','process-info','--pane',pane)
 spec={'version':1,'id':'pane-repro','name':'pane-repro','pattern':'custom','herdr':{'server':session},'members':[{'id':'pane-worker','name':'worker','role':'worker','harness':'codex','folder':home,'worktree':False,'profile':'read-only','mode':'headless','brief':'No turns allowed in this reproduction.','task':None,'host':None}],'grid':[]}
 r=cmd([binary,'app','apply-team'],{'protocol':1,'op':'apply-team','spec':spec,'if_revision':0})
 if r.returncode: raise RuntimeError('apply failed')
 start=time.monotonic()
 proc=subprocess.Popen([binary,'app','start-team'],env=env,stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True)
 proc.stdin.write(json.dumps({'protocol':1,'op':'start-team','team':'pane-repro'}));proc.stdin.close()
 time.sleep(1)
 panes=json.loads(herdr('pane','list').stdout)['result']['panes']
 for p in panes:
  if p['pane_id']!=pane:
   herdr('pane','read',p['pane_id'],'--source','recent-unwrapped','--lines','15')
 out=proc.stdout.read();err=proc.stderr.read();proc.wait()
 print('$ '+binary+' app start-team\n'+out+err+'exit='+str(proc.returncode)+' elapsed=%.3fs'%(time.monotonic()-start),flush=True)
 cmd([binary,'app','status'],{'protocol':1,'op':'status','team':'pane-repro'})
 if label == 'after':
  pathlib.Path(home+'/.zshrc').write_text('PS1="SHELL_READY> "\n')
  print('Recovery: removed the scratch startup question and retried start-team',flush=True)
  recovered=cmd([binary,'app','start-team'],{'protocol':1,'op':'start-team','team':'pane-repro'})
  assert recovered.returncode == 0
  runner=json.loads(recovered.stdout)['result']['team']['runner']
  assert runner['state']=='up' and runner['pane']
  herdr('pane','process-info','--pane',runner['pane'])
  raw=pathlib.Path('/proc/'+str(runner['pid'])+'/environ').read_bytes()
  picked={}
  for entry in raw.split(b'\x00'):
   key,_,value=entry.partition(b'=')
   if key in [b'LUGARN_COMMS_HOME',b'LUGARN_COMMS_POINTER_HOME',b'PATH']:
    picked[key.decode()]=value.decode()
  print('Runner inherited selected rootEnv values = '+json.dumps(picked),flush=True)
  assert picked['LUGARN_COMMS_HOME']==home+'/state'
  stopped=cmd([binary,'app','stop-team'],{'protocol':1,'op':'stop-team','team':'pane-repro'})
  assert stopped.returncode==0
  gone=herdr('pane','get',runner['pane'])
  assert gone.returncode!=0
finally:
 if server:
  herdr('server','stop')
  try: server.wait(timeout=10)
  except subprocess.TimeoutExpired:
   server.terminate();server.wait(timeout=5)
  herdr('session','delete',session)
 shutil.rmtree(home)
