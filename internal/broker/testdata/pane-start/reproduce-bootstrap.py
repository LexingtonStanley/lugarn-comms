import os,sys,json,time,pathlib,tempfile,secrets,subprocess,shutil
binary=sys.argv[1];home=tempfile.mkdtemp(prefix='lh-');session='lugarn-test-'+secrets.token_hex(6)
env={k:v for k,v in os.environ.items() if not k.upper().startswith('HERDR_')};env.update(HOME=home,SHELL='/usr/bin/zsh',ZDOTDIR=home,XDG_CONFIG_HOME=home+'/.config',LUGARN_COMMS_HOME=home+'/state',LUGARN_COMMS_POINTER_HOME=home)
config=pathlib.Path(home+'/.config/herdr');config.mkdir(parents=True);(config/'config.toml').write_text('[terminal]\ndefault_shell="/usr/bin/zsh"\nshell_mode="non_login"\n')
pathlib.Path(home+'/.zshrc').write_text('print -r -- STARTUP_QUESTION\nread -r answer\nprint -r -- "STARTUP_ANSWER=$answer"\nPS1="READY> "\n')
root=pathlib.Path(home+'/state/launch/01M5W85T2CP1XPA7Z3DJ8FSSZ5');root.mkdir(parents=True)
def run(argv,more=None):
 r=subprocess.run(argv,env=env| (more or {}),capture_output=True,text=True,timeout=35);print('$ '+' '.join(argv));print(r.stdout+r.stderr,end='');print('exit='+str(r.returncode));return r
def herdr(*a):return run(['herdr','--session',session,*a])
log=open(home+'/server.log','w');server=subprocess.Popen(['herdr','--session',session,'server'],env=env,stdin=subprocess.DEVNULL,stdout=log,stderr=subprocess.STDOUT,start_new_session=True)
try:
 time.sleep(.6)
 for kind in ['posix','windows-wrapper']:
  if kind=='posix':
   capsule=root/'clean-shell.json';marker=root/'shell-ready';capsule.write_text(json.dumps({'Path':env['PATH'],'Ready':str(marker)}));command='exec '+binary+' pane-shell'
  else:
   capsule=root/'pane-launch.json';marker=root/'pane-started';capsule.write_text(json.dumps({'Harness':'claude','Binary':'/usr/bin/true','Args':[],'Workdir':home,'Env':{'PATH':env['PATH']}}));command=binary+' pane-launch '+root.name
  ws=json.loads(herdr('workspace','create','--cwd',home,'--label',kind,'--env','LUGARN_COMMS_HOME='+home+'/state','--env','LUGARN_COMMS_PANE_CONFIG='+str(capsule),'--no-focus').stdout)['result'];pane=ws['root_pane']['pane_id']
  time.sleep(.3)
  herdr('pane','run',pane,command);time.sleep(.3);herdr('pane','read',pane,'--source','recent-unwrapped','--lines','12');print(kind+' bootstrap marker exists after swallowed command = '+str(marker.exists()))
  # The question has consumed the first command; the shell is now at its prompt.
  # A single manual retry demonstrates that the marker proves actual execution.
  herdr('pane','run',pane,command)
  for _ in range(100):
   if marker.exists():break
   time.sleep(.02)
  print(kind+' bootstrap marker after prompt retry = '+repr(marker.read_text() if marker.exists() else None))
  herdr('pane','close',pane)
finally:
 herdr('server','stop');server.wait(timeout=10);herdr('session','delete',session);shutil.rmtree(home)
