'use strict';
const $ = id => document.getElementById(id);
const filters = { search: '', surface:'all', status:'all' };
let inventory;
function elt(tag, cls, value) {
  const node = document.createElement(tag);
  if (cls) node.className = cls;
  if (value != null) node.textContent = value;
  return node;
}
function badge(status) {
 const label = status === 'implemented' ? '✓ FloweySales' : status === 'experimental' ? '! Experimentell' : '○ Endast upstream';
 return elt('span','status status-'+status,label);
}
function card(method,title,desc,status,meta) {
 const root=elt('article','endpoint');
 const top=elt('div','endpoint-top');
 top.append(elt('span','api-method',method),badge(status));root.append(top);
 root.append(elt('strong','api-path',title));
 if(desc)root.append(elt('p','',desc));
 if(meta)root.append(elt('div','api-meta',meta));
 return root;
}
function match(...texts){return texts.filter(Boolean).join(' ').toLocaleLowerCase('sv').includes(filters.search)}
function show(container,id,items){container.replaceChildren(...(items.length?items:[elt('p','empty','Inga poster matchar filtren.')]))}
function render(){
 const f=s=>filters.surface==='all'||filters.surface===s;
 const status=s=>filters.status==='all'||filters.status===s;
 const rest=inventory.rest.filter(x=>f('rest')&&status(x.status)&&match(x.method,x.path,x.description,x.access));
 show($('rest-results'),'rest',rest.map(x=>card(x.method,x.path,x.description,x.status,x.access)));
 const mcp=inventory.mcp.filter(x=>f('mcp')&&status(x.status)&&match(x.name,x.note));
 show($('mcp-results'),'mcp',mcp.map(x=>card('MCP tool',x.name,x.note||'Anrop via JSON-RPC tools/call',x.status,x.surface)));
 const providers=[];
 for(const group of inventory.providers){
  const items=group.endpoints.filter(x=>f('providers')&&status(x.status)&&match(group.provider,x.operation,x.path,x.via));
  if(!items.length)continue;
  const sec=elt('section','provider-section');
  const header=elt('div','provider-header');
  const left=elt('div');
  left.append(elt('h3','',group.provider));
  left.append(elt('p','provider-notice',group.auth+' · '+group.note));
  const link=elt('a','inline','Leverantörsdokumentation ↗');
  link.href=group.documentation;link.target='_blank';link.rel='noopener noreferrer';
  header.append(left,link);sec.append(header);
  const grid=elt('div','endpoint-grid');
  for(const item of items){
    const description=item.operation?'Operation: '+item.operation:'Känd leverantörsendpoint';
    grid.append(card('UPSTREAM',item.path,description,item.status,item.via));
  }
  sec.append(grid);providers.push(sec);
 }
 show($('provider-results'),'providers',providers);
 for(const [area,id] of [['rest','rest'],['mcp','mcp'],['providers','providers']]){
  $(id).hidden=filters.surface!=='all'&&filters.surface!==area;
 }
}
async function boot(){
 try{
  const r=await fetch('api-catalog.json',{cache:'no-store'});
  if(!r.ok)throw Error('HTTP '+r.status);
  inventory=await r.json();
  const live=inventory.providers.flatMap(x=>x.endpoints).filter(x=>x.status==='implemented').length;
  for(const [num,title] of [[inventory.rest.length,'REST-routes'],[inventory.mcp.length,'MCP-verktyg'],[live,'Providerkopplingar']]){
   const m=elt('div','metric');m.append(elt('strong','',String(num)),elt('span','',title));$('metrics').append(m);
  }
  $('source-results').replaceChildren(...inventory.sources.map(source=>{
   const a=elt('a','',source.label+' ↗');a.href=source.url;a.target='_blank';a.rel='noopener noreferrer';return a;
  }));
  $('filter-search').addEventListener('input',e=>{filters.search=e.target.value.trim().toLocaleLowerCase('sv');render()});
  $('filter-surface').addEventListener('change',e=>{filters.surface=e.target.value;render()});
  $('filter-status').addEventListener('change',e=>{filters.status=e.target.value;render()});
  render();
 }catch(_){$('catalog-error').hidden=false;}
}
boot();
