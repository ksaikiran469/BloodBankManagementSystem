import {useEffect,useState,type FormEvent} from 'react';

type Hospital={id:number;name:string;city:string;email:string;phone:string};
type Stock={id:number;hospital_id:number;hospital_name:string;blood_group:string;units:number;low_threshold:number;updated_at:string};
const groups=['A+','A-','B+','B-','AB+','AB-','O+','O-'];

async function request(url:string,opts?:RequestInit){
 const r=await fetch(url,opts);
 const text=await r.text();
 let d:unknown=null;
 try{d=text?JSON.parse(text):null}catch{throw new Error('Invalid API response. Verify the Go backend is running.')}
 if(!r.ok)throw new Error((d as {error?:string}|null)?.error||`Request failed (${r.status})`);
 return d;
}

export default function Stage3({page}:{page:'hospitals'|'inventory'}){
 const [hospitals,setHospitals]=useState<Hospital[]>([]);
 const [stocks,setStocks]=useState<Stock[]>([]);
 const [message,setMessage]=useState('');
 const [hospital,setHospital]=useState({name:'',city:'',email:'',phone:''});
 const [editingHospitalId,setEditingHospitalId]=useState<number|null>(null);
 const [stock,setStock]=useState({hospital_id:0,blood_group:'O+',units:0,low_threshold:5});
 const [editingStockId,setEditingStockId]=useState<number|null>(null);

 const load=async()=>{
  try{
   const [h,s]=await Promise.all([request('/api/v1/hospitals'),request('/api/v1/inventory')]);
   setHospitals(h as Hospital[]);setStocks(s as Stock[]);
  }catch(e){setMessage(String(e))}
 };
 useEffect(()=>{void load()},[]);

 const saveHospital=async(e:FormEvent)=>{
  e.preventDefault();setMessage('');
  try{
   await request(editingHospitalId===null?'/api/v1/hospitals':`/api/v1/hospitals/${editingHospitalId}`,{
    method:editingHospitalId===null?'POST':'PUT',
    headers:{'Content-Type':'application/json'},body:JSON.stringify(hospital)
   });
   setHospital({name:'',city:'',email:'',phone:''});
   setMessage(editingHospitalId===null?'Hospital registered':'Hospital updated');
   setEditingHospitalId(null);await load();
  }catch(e){setMessage(String(e))}
 };
 const editHospital=(h:Hospital)=>{
  setEditingHospitalId(h.id);setHospital({name:h.name,city:h.city,email:h.email,phone:h.phone});
  window.scrollTo({top:0,behavior:'smooth'});
 };
 const removeHospital=async(h:Hospital)=>{
  if(!window.confirm(`Delete ${h.name}? Hospitals with inventory or blood requests cannot be deleted.`))return;
  setMessage('');
  try{await request(`/api/v1/hospitals/${h.id}`,{method:'DELETE'});setMessage('Hospital deleted');await load()}
  catch(e){setMessage(String(e))}
 };
 const saveStock=async(e:FormEvent)=>{
  e.preventDefault();setMessage('');
  try{
   if(editingStockId===null){
    await request('/api/v1/inventory',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify(stock)});
   }else{
    await request(`/api/v1/inventory/${editingStockId}`,{method:'PUT',headers:{'Content-Type':'application/json'},body:JSON.stringify({units:stock.units,low_threshold:stock.low_threshold})});
   }
   setMessage(editingStockId===null?'Inventory saved':'Inventory updated');
   setEditingStockId(null);setStock({hospital_id:0,blood_group:'O+',units:0,low_threshold:5});await load();
  }catch(e){setMessage(String(e))}
 };
 const editStock=(s:Stock)=>{
  setEditingStockId(s.id);setStock({hospital_id:s.hospital_id,blood_group:s.blood_group,units:s.units,low_threshold:s.low_threshold});
  window.scrollTo({top:0,behavior:'smooth'});
 };
 const removeStock=async(s:Stock)=>{
  if(!window.confirm(`Delete ${s.blood_group} inventory for ${s.hospital_name}? This cannot be undone.`))return;
  setMessage('');
  try{await request(`/api/v1/inventory/${s.id}`,{method:'DELETE'});setMessage('Inventory item deleted');await load()}
  catch(e){setMessage(String(e))}
 };
 const adjust=async(s:Stock,delta:number)=>{
  try{await request(`/api/v1/inventory/${s.id}/adjust`,{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({delta})});setMessage('Stock adjusted');await load()}
  catch(e){setMessage(String(e))}
 };

 return <>
  {page==='hospitals'?<>
   <section className="panel">
    <h3>{editingHospitalId===null?'Register hospital':'Edit hospital'}</h3>
    <form className="donor-form" onSubmit={saveHospital}>
     {(['name','city','email','phone'] as const).map(k=><label key={k}>{k[0].toUpperCase()+k.slice(1)}<input required type={k==='email'?'email':'text'} value={hospital[k]} onChange={e=>setHospital(p=>({...p,[k]:e.target.value}))}/></label>)}
     <div className="actions"><button type="submit">{editingHospitalId===null?'Register hospital':'Save changes'}</button>{editingHospitalId!==null&&<button type="button" className="secondary" onClick={()=>{setEditingHospitalId(null);setHospital({name:'',city:'',email:'',phone:''})}}>Cancel</button>}</div>
    </form>
   </section>
   <section className="panel">
    <h3>Hospital directory</h3>
    <div className="table-scroll"><table><thead><tr><th>Name</th><th>City</th><th>Email</th><th>Phone</th><th>Actions</th></tr></thead><tbody>
     {hospitals.map(h=><tr key={h.id}><td>{h.name}</td><td>{h.city}</td><td>{h.email}</td><td>{h.phone}</td><td><div className="actions"><button className="secondary" onClick={()=>editHospital(h)}>Edit</button><button className="secondary" onClick={()=>void removeHospital(h)}>Delete</button></div></td></tr>)}
    </tbody></table></div>
    {hospitals.length===0&&<p>No hospitals registered yet.</p>}
   </section>
  </>:<>
   <section className="panel">
    <h3>{editingStockId===null?'Set inventory for a hospital':'Edit inventory item'}</h3>
    {editingStockId===null&&<p>Saving an existing hospital and blood-group combination replaces its current units and threshold. Use +/− below for incremental changes.</p>}
    <form className="donor-form" onSubmit={saveStock}>
     <label>Hospital<select required disabled={editingStockId!==null} value={stock.hospital_id||''} onChange={e=>setStock(p=>({...p,hospital_id:Number(e.target.value)}))}><option value="">Choose hospital</option>{hospitals.map(h=><option key={h.id} value={h.id}>{h.name} — {h.city}</option>)}</select></label>
     <label>Blood group<select disabled={editingStockId!==null} value={stock.blood_group} onChange={e=>setStock(p=>({...p,blood_group:e.target.value}))}>{groups.map(g=><option key={g}>{g}</option>)}</select></label>
     <label>Units<input required type="number" min="0" value={stock.units} onChange={e=>setStock(p=>({...p,units:Number(e.target.value)}))}/></label>
     <label>Low-stock threshold<input required type="number" min="0" value={stock.low_threshold} onChange={e=>setStock(p=>({...p,low_threshold:Number(e.target.value)}))}/></label>
     <div className="actions"><button type="submit" disabled={hospitals.length===0}>{editingStockId===null?'Save inventory':'Update inventory'}</button>{editingStockId!==null&&<button type="button" className="secondary" onClick={()=>{setEditingStockId(null);setStock({hospital_id:0,blood_group:'O+',units:0,low_threshold:5})}}>Cancel</button>}</div>
    </form>
    {hospitals.length===0&&<p>Register a hospital first.</p>}
   </section>
   <section className="panel">
    <h3>Inventory overview</h3><p>Low stock: {stocks.filter(s=>s.units<s.low_threshold).length} group(s) below threshold.</p>
    <div className="table-scroll"><table><thead><tr><th>Hospital</th><th>Group</th><th>Units</th><th>Threshold</th><th>Status</th><th>Adjust units</th><th>Actions</th></tr></thead><tbody>
     {stocks.map(s=><tr key={s.id}><td>{s.hospital_name}</td><td><b>{s.blood_group}</b></td><td>{s.units}</td><td>{s.low_threshold}</td><td><span className={s.units<s.low_threshold?'stock-low':'stock-ok'}>{s.units<s.low_threshold?'Low stock':'OK'}</span></td><td><button className="secondary" onClick={()=>void adjust(s,-1)} disabled={s.units===0}>−1</button> <button className="secondary" onClick={()=>void adjust(s,1)}>+1</button></td><td><div className="actions"><button className="secondary" onClick={()=>editStock(s)}>Edit</button><button className="secondary" onClick={()=>void removeStock(s)}>Delete</button></div></td></tr>)}
    </tbody></table></div>
    {stocks.length===0&&<p>No inventory yet.</p>}
   </section>
  </>}
  {message&&<p role="status" className="notice">{message}</p>}
 </>;
}
