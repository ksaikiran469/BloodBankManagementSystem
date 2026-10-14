import React,{useEffect,useState} from 'react';
type Session={token:string;role:'admin'|'donor';email:string};
type DonorProfile={id:number;full_name:string;blood_group:string;city:string;email:string;phone:string;available:boolean;created_at:string};
type DonorInput=Pick<DonorProfile,'full_name'|'blood_group'|'city'|'phone'|'available'>;
const groups=['A+','A-','B+','B-','AB+','AB-','O+','O-'];
export function getSession():Session|null{
 try{
  const session=JSON.parse(sessionStorage.getItem('bloodconnect_session')||'null') as Session|null;
  return session&&(session.role==='admin'||session.role==='donor')?session:null;
 }catch{return null}
}
// All existing Stage 1–4 fetch calls gain an Authorization header without rewriting each page.
const originalFetch=window.fetch.bind(window);
window.fetch=(input:RequestInfo|URL,init?:RequestInit)=>{
 const url=typeof input==='string'?input:input instanceof URL?input.toString():input.url;
 if(!url.startsWith('/api/v1/')||url==='/api/v1/auth/login'||url==='/api/v1/auth/register')return originalFetch(input,init);
 const headers=new Headers(init?.headers || (input instanceof Request?input.headers:undefined));const token=getSession()?.token;if(token)headers.set('Authorization','Bearer '+token);
 return originalFetch(input,{...init,headers});
};
export default function AuthGate({children}:{children:React.ReactNode}){
 const [session,setSession]=useState<Session|null>(getSession());const [portal,setPortal]=useState<'admin'|'donor'>('admin');const [mode,setMode]=useState<'login'|'register'>('login');
 const [email,setEmail]=useState(''),[password,setPassword]=useState(''),[fullName,setFullName]=useState(''),[bloodGroup,setBloodGroup]=useState('O+'),[city,setCity]=useState(''),[phone,setPhone]=useState('');
 const [error,setError]=useState(''),[busy,setBusy]=useState(false);
 async function submit(e:React.FormEvent){
  e.preventDefault();setError('');setBusy(true);
  try{
   const body=mode==='register'?{email,password,full_name:fullName,blood_group:bloodGroup,city,phone}:{email,password,expected_role:portal};
   const r=await originalFetch('/api/v1/auth/'+(mode==='register'?'register':'login'),{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(body)});
   const text=await r.text();const data=text?JSON.parse(text):{};if(!r.ok)throw Error(data.error||'Authentication failed');
   if(mode==='register'){setMode('login');setError('Donor account created. Please sign in.')}
   else{const s:Session={token:data.token,role:data.role,email:data.email};sessionStorage.setItem('bloodconnect_session',JSON.stringify(s));setSession(s)}
  }catch(e){setError(String(e))}finally{setBusy(false)}
 }
 if(session?.role==='admin')return <>{children}</>;
 if(session?.role==='donor')return <DonorPortal session={session}/>;
 return <div className="auth-page"><section className="panel auth-panel"><h1>♥ Blood Bank</h1><h2>{portal==='admin'?'Admin sign in':mode==='login'?'Donor sign in':'Create donor account'}</h2>
  <div className="auth-portals"><button type="button" className={portal==='admin'?'':'secondary'} onClick={()=>{setPortal('admin');setMode('login');setError('')}}>Admin login</button><button type="button" className={portal==='donor'?'':'secondary'} onClick={()=>{setPortal('donor');setMode('login');setError('')}}>Donor login</button></div>
  <form onSubmit={submit} className="donor-form">
   {mode==='register'&&<><label>Full name<input required minLength={2} value={fullName} onChange={e=>setFullName(e.target.value)}/></label><label>Blood group<select value={bloodGroup} onChange={e=>setBloodGroup(e.target.value)}>{groups.map(g=><option key={g}>{g}</option>)}</select></label><label>City<input required value={city} onChange={e=>setCity(e.target.value)}/></label><label>Phone<input required value={phone} onChange={e=>setPhone(e.target.value)}/></label></>}
   <label>Email<input type="email" required value={email} onChange={e=>setEmail(e.target.value)}/></label><label>Password<input type="password" required minLength={portal==='admin'?10:12} maxLength={72} value={password} onChange={e=>setPassword(e.target.value)}/></label>
   <button disabled={busy}>{busy?'Please wait':mode==='register'?'Create donor account':'Log in'}</button>
  </form>
  {portal==='donor'&&<p>{mode==='login'?'New donor?':'Already registered?'} <button className="secondary" type="button" onClick={()=>{setMode(mode==='login'?'register':'login');setError('')}}>{mode==='login'?'Create donor login':'Donor login'}</button></p>}
  {portal==='admin'&&<p className="muted">Admin accounts are provisioned by the application owner.</p>}
  {error&&<p role="status" className="notice">{error}</p>}
 </section></div>;
}
function DonorPortal({session}:{session:Session}){
 const [profile,setProfile]=useState<DonorProfile|null>(null);const [form,setForm]=useState<DonorInput|null>(null);const [error,setError]=useState('');const [notice,setNotice]=useState('');const [busy,setBusy]=useState(false);
 async function load(){
  try{const response=await fetch('/api/v1/donors/me');const data=await response.json();if(!response.ok)throw Error(data.error||'Could not load donor profile');setProfile(data);setForm({full_name:data.full_name,blood_group:data.blood_group,city:data.city,phone:data.phone,available:data.available});setError('')}
  catch(e){setError(String(e))}
 }
 useEffect(()=>{void load()},[]);
 async function save(e:React.FormEvent){
  e.preventDefault();if(!form)return;setBusy(true);setError('');setNotice('');
  try{const response=await fetch('/api/v1/donors/me',{method:'PUT',headers:{'Content-Type':'application/json'},body:JSON.stringify(form)});const data=await response.json();if(!response.ok)throw Error(data.error||'Could not save donor profile');setProfile(data);setForm({full_name:data.full_name,blood_group:data.blood_group,city:data.city,phone:data.phone,available:data.available});setNotice('Your donor profile was updated')}
  catch(e){setError(String(e))}finally{setBusy(false)}
 }
 function logout(){sessionStorage.removeItem('bloodconnect_session');location.reload()}
 return <div className="auth-page"><main className="donor-portal"><header><div><p className="eyebrow">BLOODCONNECT · DONOR PORTAL</p><h1>Your donor profile</h1><p className="muted">You can access and update only your own donor information.</p></div><button className="secondary" onClick={logout}>Log out</button></header>
  {profile&&form&&<section className="panel"><p className="muted">Signed in as {session.email}</p><form className="donor-form" onSubmit={save}><label>Full name<input required minLength={2} value={form.full_name} onChange={e=>setForm({...form,full_name:e.target.value})}/></label><label>Blood group<select value={form.blood_group} onChange={e=>setForm({...form,blood_group:e.target.value})}>{groups.map(g=><option key={g}>{g}</option>)}</select></label><label>City<input required value={form.city} onChange={e=>setForm({...form,city:e.target.value})}/></label><label>Email<input type="email" value={profile.email} readOnly/></label><label>Phone<input required value={form.phone} onChange={e=>setForm({...form,phone:e.target.value})}/></label><label className="check"><input type="checkbox" checked={form.available} onChange={e=>setForm({...form,available:e.target.checked})}/> Available to contact (demo)</label><div><button disabled={busy}>{busy?'Saving…':'Save my profile'}</button></div></form></section>}
  {!profile&&!error&&<section className="panel">Loading your donor profile…</section>}{notice&&<p className="notice" role="status">{notice}</p>}{error&&<p className="notice" role="alert">{error}</p>}
 </main></div>;
}
