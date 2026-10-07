const {test}=require('node:test');
const assert=require('node:assert/strict');
const fs=require('node:fs');
const path=require('node:path');
const vm=require('node:vm');

function entorno(){
 const dialog={open:true,querySelectorAll:()=>[{disabled:false}],close(){this.open=false;}};
 const select={value:'2',focus(){}};
 const applied=[];
 const sandbox={
  document:{addEventListener(){},getElementById:id=>id==='dialogo-vincular'?dialog:select},
  api:{},estado:{seccion:'pcs'},mensajeDeError:e=>e.message,mostrarAlerta(){},avisar(){},aplicarCambio:item=>applied.push(item),
 };
 vm.createContext(sandbox);
 vm.runInContext(fs.readFileSync(path.join(__dirname,'../static/js/equipment.js'),'utf8'),sandbox);
 vm.runInContext('renderEquiposPC=()=>{}; enfocarEquipoPC=()=>{}; globalThis.prueba={equiposPC,vinculoPC,cargarEquiposPC,invalidarEquipos,guardarVinculoPC};',sandbox);
 return {...sandbox.prueba,api:sandbox.api,estado:sandbox.estado,dialog,applied};
}

test('equipos descarta respuestas viejas y mantiene la seleccion actual',async()=>{
 const e=entorno();let resolve;
 e.api.getEquipments=()=>new Promise(r=>{resolve=r;});
 const old=e.cargarEquiposPC();
 e.equiposPC.seleccionado=2;
 e.api.getEquipments=async()=>[{gabinete:{id:2},componentes:[]}];
 await e.cargarEquiposPC();resolve([{gabinete:{id:1},componentes:[]}]);await old;
 assert.equal(e.equiposPC.datos[0].gabinete.id,2);
 assert.equal(e.equiposPC.seleccionado,2);
});

test('respuesta al salir de equipos no altera la nueva vista',async()=>{
 const e=entorno();let resolve;
 e.api.getEquipments=()=>new Promise(r=>{resolve=r;});
 const pending=e.cargarEquiposPC();e.estado.seccion='equipos';
 resolve([{gabinete:{id:1},componentes:[]}]);await pending;
 assert.equal(e.equiposPC.datos,null);
});

test('equipos permite reintentar carga y no conserva seleccion eliminada',async()=>{
 const e=entorno();e.equiposPC.seleccionado=1;
 e.api.getEquipments=async()=>{throw new Error('Sin conexion');};
 await e.cargarEquiposPC();assert.equal(e.equiposPC.error,'Sin conexion');
 e.api.getEquipments=async()=>[];await e.cargarEquiposPC();
 assert.equal(e.equiposPC.error,'');assert.equal(e.equiposPC.seleccionado,null);
});

test('vinculacion bloquea doble envio y conserva numero recibido',async()=>{
 const e=entorno();let resolve,calls=0;
 e.vinculoPC.equipo={id:1};
 e.api.setEquipment=(id,parent)=>{calls++;assert.equal(id,2);assert.equal(parent,1);return new Promise(r=>{resolve=r;});};
 const event={preventDefault(){}};
 const pending=e.guardarVinculoPC(event);await e.guardarVinculoPC(event);
 assert.equal(calls,1);
 resolve({id:2,equipo_id:1,numero_inventario:'MON-10'});await pending;
 assert.equal(e.applied[0].numero_inventario,'MON-10');
 assert.equal(e.dialog.open,false);assert.equal(e.vinculoPC.enviando,false);
});

test('error de vinculacion mantiene el dialogo y permite corregir',async()=>{
 const e=entorno();e.vinculoPC.equipo={id:1};
 e.api.setEquipment=async()=>{throw new Error('Numero compartido');};
 await e.guardarVinculoPC({preventDefault(){}});
 assert.equal(e.dialog.open,true);assert.equal(e.vinculoPC.enviando,false);assert.equal(e.applied.length,0);
});
