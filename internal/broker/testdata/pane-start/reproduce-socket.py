import os,sys,subprocess,tempfile,pathlib,secrets,json,time,shutil
binary=sys.argv[1]
for case in ['overlong-api','overlong-client-only']:
 base=tempfile.mkdtemp(prefix='ls-');session='lugarn-test-'+secrets.token_hex(6)
 suffix='/.config/herdr/sessions/'+session+'/herdr.sock'
 extra=90 if case=='overlong-api' else 104-len(suffix)-len(base)-1
 home=base+'/'+('h'*extra);pathlib.Path(home).mkdir()
 env={k:v for k,v in os.environ.items() if not k.upper().startswith('HERDR_')};env.update(HOME=home,XDG_CONFIG_HOME=home+'/.config',SHELL='/bin/sh',LUGARN_COMMS_HOME=base+'/state',LUGARN_COMMS_POINTER_HOME=home)
 request={'protocol':1,'op':'start-herdr','server':session}
 start=time.monotonic();p=subprocess.run([binary,'app','start-herdr'],input=json.dumps(request),env=env,text=True,capture_output=True,timeout=20);duration=time.monotonic()-start
 print(case+' HOME='+home+'\n$ '+binary+' app start-herdr\n'+json.dumps(request)+'\n'+p.stdout+p.stderr+'exit='+str(p.returncode)+' elapsed=%.3fs'%duration)
 response=json.loads(p.stdout);assert response['error']['code']=='herdr_unavailable' and response['error']['details']['reason']=='socket_path_too_long';assert duration<2
 assert response['error']['details']['path'].endswith('herdr.sock' if case=='overlong-api' else 'herdr-client.sock')
 for args in [('status','--json'),('session','delete',session)]:
  r=subprocess.run(['herdr','--session',session,*args],env=env,text=True,capture_output=True,timeout=10);print('$ herdr --session '+session+' '+' '.join(args)+'\n'+r.stdout+r.stderr+'exit='+str(r.returncode))
 shutil.rmtree(base)
