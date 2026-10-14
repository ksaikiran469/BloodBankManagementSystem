import Stage3 from './stage3';
import AuthGate, {getSession} from './stage5';
import Stage4 from './stage4';
import Reports from './reports';
import React, {useEffect, useState} from 'react';
import {createRoot} from 'react-dom/client';
import './style.css';

type Donor = {id:number;full_name:string;blood_group:string;city:string;email:string;phone:string;available:boolean;created_at:string};
type Input = Omit<Donor,'id'|'created_at'>;
const empty:Input = {full_name:'',blood_group:'O+',city:'',email:'',phone:'',available:true};
const groups=['A+','A-','B+','B-','AB+','AB-','O+','O-'];

function App(){
 const [page,setPage]=useState<'overview'|'donors'|'hospitals'|'inventory'|'requests'|'notifications'|'reports'>('overview');
 const [donors,setDonors]=useState<Donor[]>([]);
 const [form,setForm]=useState<Input>(empty);
 const [editing,setEditing]=useState<number|null>(null);
 const [filters,setFilters]=useState({name:'',city:'',blood_group:''});
 const [message,setMessage]=useState('');
 const [loading,setLoading]=useState(false);

 const load=async()=>{
  setLoading(true);
  try{
   const q=new URLSearchParams(Object.entries(filters).filter(([,v])=>v!=="") as [string,string][]);
   const r=await fetch('/api/v1/donors?'+q);
   if(!r.ok)throw Error('Could not load donors');
   setDonors(await r.json());
  }catch(e){setMessage(String(e))}
  finally{setLoading(false)}
 };
 useEffect(()=>{void load()},[filters.name,filters.city,filters.blood_group]);

 const submit=async(e:React.FormEvent)=>{
  e.preventDefault();setMessage('');
  try{
   const r=await fetch(editing===null?'/api/v1/donors':`/api/v1/donors/${editing}`,{
    method:editing===null?'POST':'PUT',
    headers:{'Content-Type':'application/json'},
    body:JSON.stringify(form)
   });
   const data=await r.json();
   if(!r.ok)throw Error(data.error||'Save failed');
   setMessage(editing===null?'Donor registered':'Donor updated');
   setEditing(null);setForm({...empty});await load();
  }catch(e){setMessage(String(e))}
 };
 const edit=(d:Donor)=>{
  setEditing(d.id);
  setForm({full_name:d.full_name,blood_group:d.blood_group,city:d.city,email:d.email,phone:d.phone,available:d.available});
  window.scrollTo({top:0,behavior:'smooth'});
 };
 const removeDonor=async(d:Donor)=>{
  if(!window.confirm(`Delete ${d.full_name} and their donor login account? This cannot be undone.`))return;
  setMessage('');
  try{
   const r=await fetch(`/api/v1/donors/${d.id}`,{method:'DELETE'});
   const data=await r.json();
   if(!r.ok)throw Error(data.error||'Delete failed');
   if(editing===d.id){setEditing(null);setForm({...empty})}
   setMessage('Donor and linked login deleted');await load();
  }catch(e){setMessage(String(e))}
 };
 const update=(key:keyof Input,value:string|boolean)=>setForm(p=>({...p,[key]:value}));

 return <div className="app">
  <aside>
   <div className="brand"><span className="brand-mark">♥</span><span>BloodBank Management System</span></div>
   <div className="sidebar-label">WORKSPACE</div>
   <div className={'nav '+(page==='overview'?'active':'')} onClick={()=>setPage('overview')}>Overview</div>
   <div className={'nav '+(page==='donors'?'active':'')} onClick={()=>setPage('donors')}>Donors</div>
   <div className={'nav '+(page==='hospitals'?'active':'')} onClick={()=>setPage('hospitals')}>Hospitals</div>
   <div className={'nav '+(page==='inventory'?'active':'')} onClick={()=>setPage('inventory')}>Inventory</div>
   <div className={'nav '+(page==='requests'?'active':'')} onClick={()=>setPage('requests')}>Requests</div>
   <div className={'nav '+(page==='notifications'?'active':'')} onClick={()=>setPage('notifications')}>Notifications</div>
   {getSession()?.role==='admin'&&<div className={'nav '+(page==='reports'?'active':'')} onClick={()=>setPage('reports')}>Reports & Analytics</div>}
   <div className="sidebar-bottom"><span className="sidebar-dot"/> System workspace</div>
  </aside>
  <main>
   <header>
    <div><p className="eyebrow">OPERATIONS CENTER · BLOODBANK</p><h1>{page==='donors'?'Donor Management':page==='hospitals'?'Hospital Management':page==='inventory'?'Blood Inventory':page==='requests'?'Blood Requests':page==='notifications'?'Notifications':page==='reports'?'Reports & Analytics':'Overview Dashboard'}</h1></div>
    <div><span className="pill">{getSession()?.role||"Demo"}</span> <button className="secondary" onClick={()=>{sessionStorage.removeItem("bloodconnect_session");location.reload()}}>Log out</button></div>
   </header>
   {(page==='hospitals'||page==='inventory')?<Stage3 page={page}/>:page==='requests'?<Stage4/>:page==='notifications'?<Notifications/>:page==='overview'?<>
    <section className="hero"><div><p className="eyebrow">WELCOME TO BLOOD BANK</p><h2>Every drop connects a life.</h2><p>Manage donors, hospital partners, blood inventory and requests from one connected workspace.</p><button className="hero-button" onClick={()=>setPage('requests')}>Manage blood requests →</button></div><div className="hero-art"><div className="hero-heart">♥</div><span>Care, connected.</span></div></section>
    {getSession()?.role==='admin'&&<Reports overview/>}
   </>:page==='reports'&&getSession()?.role==='admin'?<Reports/>:<>
    <section className="panel">
     <h3>{editing===null?'Register a donor':'Edit donor #'+editing}</h3>
     <form className="donor-form" onSubmit={submit}>
      <label>Full name<input required minLength={2} value={form.full_name} onChange={e=>update('full_name',e.target.value)}/></label>
      <label>Blood group<select value={form.blood_group} onChange={e=>update('blood_group',e.target.value)}>{groups.map(g=><option key={g}>{g}</option>)}</select></label>
      <label>City<input required value={form.city} onChange={e=>update('city',e.target.value)}/></label>
      <label>Email<input required type="email" value={form.email} onChange={e=>update('email',e.target.value)}/></label>
      <label>Phone<input required value={form.phone} onChange={e=>update('phone',e.target.value)}/></label>
      <label className="check"><input type="checkbox" checked={form.available} onChange={e=>update('available',e.target.checked)}/> Available to contact (demo)</label>
      <div className="actions"><button type="submit">{editing===null?'Register donor':'Save changes'}</button>{editing!==null&&<button type="button" className="secondary" onClick={()=>{setEditing(null);setForm({...empty})}}>Cancel</button>}</div>
     </form>
    </section>
    <section className="panel">
     <h3>Donor directory</h3>
     <div className="filters">
      <input placeholder="Search name" value={filters.name} onChange={e=>setFilters(p=>({...p,name:e.target.value}))}/>
      <input placeholder="Filter city" value={filters.city} onChange={e=>setFilters(p=>({...p,city:e.target.value}))}/>
      <select value={filters.blood_group} onChange={e=>setFilters(p=>({...p,blood_group:e.target.value}))}><option value="">All blood groups</option>{groups.map(g=><option key={g}>{g}</option>)}</select>
     </div>
     <div className="table-scroll"><table><thead><tr><th>Name</th><th>Group</th><th>City</th><th>Email</th><th>Available</th><th>Actions</th></tr></thead><tbody>
      {donors.map(d=><tr key={d.id}><td>{d.full_name}</td><td><b>{d.blood_group}</b></td><td>{d.city}</td><td>{d.email}</td><td>{d.available?'Yes':'No'}</td><td><div className="actions"><button className="secondary" onClick={()=>edit(d)}>Edit</button><button className="secondary" onClick={()=>void removeDonor(d)}>Delete</button></div></td></tr>)}
     </tbody></table></div>
     {loading&&<p>Loading...</p>}{!loading&&donors.length===0&&<p className="muted">No donors match your search.</p>}
    </section>
   </>}
   {message&&<p role="status" className="notice">{message}</p>}
   <footer>© BloodBank</footer>
  </main>
 </div>;
}

function Notifications(){
 const [items,setItems]=useState<{type:string;message:string}[]>([]);
 const [error,setError]=useState('');
 const refresh=async()=>{
  try{const r=await fetch('/api/v1/notifications');if(!r.ok)throw Error('Could not load notifications');setItems(await r.json());setError('')}
  catch(e){setError(String(e))}
 };
 useEffect(()=>{void refresh()},[]);
 return <section className="panel"><h3>Live alerts</h3><p className="muted">Calculated from current inventory and request status. Refresh to see changes.</p><button onClick={()=>void refresh()}>Refresh notifications</button>{error&&<p role="alert">{error}</p>}{items.length===0&&<p>No active alerts.</p>}{items.map((x,i)=><p className="notice" key={i}>{x.message}</p>)}</section>;
}

createRoot(document.getElementById('root')!).render(<React.StrictMode><AuthGate><App/></AuthGate></React.StrictMode>);
