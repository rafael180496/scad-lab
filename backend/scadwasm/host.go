package scadwasm

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"

	"scadlab/backend/i18n"
)

// proc es el estado de UNA ejecución de OpenSCAD: su árbol de archivos, la
// salida, el entorno y las excepciones de C++ en vuelo. Las funciones del host
// lo encuentran por el contexto (procOf): el módulo del host es uno solo y lo
// comparten todas las instancias en paralelo.
type proc struct {
	fs     *vfs
	out    *tailBuffer
	args   []string
	env    []string
	mounts []Mount // los montajes del disco, normalizados: para reescribir la salida

	mod api.Module
	mem api.Memory

	// nctx es el contexto de la ejecución SIN cancelación. Las llamadas del
	// host al módulo (cada invoke_*, setThrew, stackSave…) lo usan en vez del
	// contexto de Run: con WithCloseOnContextDone, wazero lanza una goroutine
	// por cada llamada con un contexto cancelable, y un invoke_* se ejecuta
	// millones de veces. La cancelación sigue funcionando: la vigila la llamada
	// de más arriba (main), y cerrar el módulo corta también las anidadas.
	nctx context.Context

	start time.Time

	// Exports de emscripten que el host necesita (nombres minificados de este
	// build; ver exportNames).
	fnSetThrew, fnStackSave, fnStackRestore, fnStackAlloc api.Function
	fnTempRetSet, fnCanCatch, fnIncRef, fnDecRef          api.Function
	fnGetExcPtr, fnMemalign                               api.Function

	// Excepciones de C++ (el modelo de emscripten con -fexceptions: se lanzan
	// «por JavaScript», aquí por panic, y las capturan los invoke_*).
	exceptionLast uint32
	uncaught      int32
	caught        []uint32 // pila de excepciones capturadas (excPtr)

	// throwing: hay una excepción de C++ (o un longjmp) en vuelo, que el invoke_*
	// más cercano tiene que atrapar. fatal: el programa pidió terminar o
	// abortó, y nada lo atrapa.
	throwing bool
	fatal    error

	// scratch evita una asignación por cada llamada a un export corto. Es
	// seguro porque esos exports no llaman a ninguna función del host que lo
	// use a su vez.
	scratch [4]uint64

	// pool de api.Function de la tabla indirecta: crear una cuesta una pila de
	// 10 KB, y los invoke_* se llaman millones de veces.
	fnPool map[uint64][]tableFn
}

// Nombres (minificados) de los exports de este build; salen de
// assignWasmExports del openscad.js de la misma versión.
const (
	expMemory       = "Tb"
	expCtors        = "Ub"
	expMain         = "Vb"
	expTable        = "Wb"
	expFflush       = "Xb"
	expGetExcPtr    = "Yb"
	expFuncsOnExit  = "Zb"
	expMemalign     = "_b"
	expSetThrew     = "$b"
	expTempRetSet   = "ac"
	expStackRestore = "bc"
	expStackAlloc   = "cc"
	expStackGet     = "dc"
	expDecRef       = "ec"
	expIncRef       = "fc"
	expCanCatch     = "gc"
)

// Señales de control que viajan como panic a través del código del módulo (el
// invoke_* que corresponda las atrapa o las deja pasar).
type (
	// throwSignal: una excepción de C++ o un longjmp en vuelo.
	throwSignal struct{}
	// exitSignal: el programa pidió terminar (exit()).
	exitSignal struct{ code int32 }
	// abortSignal: assert o abort() del programa.
	abortSignal struct{ msg string }
)

func (throwSignal) Error() string   { return "c++ exception / longjmp" }
func (e *exitSignal) Error() string { return fmt.Sprintf("exit(%d)", e.code) }
func (e *abortSignal) Error() string {
	if e.msg == "" {
		return "abort()"
	}
	return "abort: " + e.msg
}

// runGuarded es run con la red de seguridad de que un panic del host (un bug)
// no tumba el proceso desde una goroutine suelta.
func (p *proc) runGuarded(ctx context.Context, mod api.Module) (code int, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = i18n.Errorf(i18n.Msg{ES: "fallo interno del motor integrado: %v", EN: "internal error in the built-in engine: %v"}, r)
		}
	}()
	return p.run(ctx, mod)
}

// throw lanza una excepción de C++ / longjmp: lo atrapa el invoke_* que la rodea.
func (p *proc) throw() {
	p.throwing = true
	panic(throwSignal{})
}

// die termina la ejecución (exit, abort, assert): ningún invoke_* lo atrapa.
func (p *proc) die(err error) {
	p.fatal = err
	panic(err)
}

// hostFn es la firma de una función del host.
type hostFn func(ctx context.Context, mod api.Module, stack []uint64)

// tableImpls son las funciones del host que necesitan buscar en la tabla
// indirecta (no son estáticas: dependen del tableLookup del módulo).
var tableImpls = map[string]func(tl *tableLookup) hostFn{
	// ___call_sighandler(fp, sig): llama a un manejador de señal registrado.
	"___call_sighandler": func(tl *tableLookup) hostFn {
		return func(ctx context.Context, mod api.Module, st []uint64) {
			fn := tl.lookup(mod, uint32(st[0]), []api.ValueType{api.ValueTypeI32}, nil)
			if err := fn(ctx, []uint64{st[1]}); err != nil {
				panic(err)
			}
		}
	},
}

// hostImpls son las funciones del host por su nombre real (el de emscripten).
// Los invoke_* no están: se generan según la firma de cada importación.
var hostImpls = map[string]hostFn{}

// registerHost crea el módulo "a" con las 153 importaciones del wasm. Los
// invoke_* se generan a partir de la firma de cada importación.
func registerHost(ctx context.Context, rt wazero.Runtime, cm wazero.CompiledModule, wasm []byte) error {
	slots, err := hostSlots(wasm)
	if err != nil {
		return err
	}
	hb := rt.NewHostModuleBuilder("a")
	var missing []string
	var nInvoke uint32
	tl := &tableLookup{host: map[uint32]hostFn{}}
	defs := cm.ImportedFunctions()
	// Las funciones del host que el módulo guarda en su tabla indirecta.
	for slot, imp := range slots {
		if int(imp) >= len(defs) {
			continue
		}
		_, key, _ := defs[imp].Import()
		name := importNames[key]
		if fn := hostImpls[name]; fn != nil {
			tl.host[slot] = fn
		}
	}
	for _, def := range defs {
		modName, key, _ := def.Import()
		if modName != "a" {
			return i18n.Errorf(i18n.Msg{ES: "scadwasm: importación inesperada %s.%s", EN: "scadwasm: unexpected import %s.%s"}, modName, key)
		}
		name, ok := importNames[key]
		if !ok {
			missing = append(missing, key)
			continue
		}
		params, results := def.ParamTypes(), def.ResultTypes()
		var fn hostFn
		if mk := tableImpls[name]; mk != nil {
			fn = mk(tl)
		} else if strings.HasPrefix(name, "invoke_") {
			nInvoke++
			fn = makeInvoke(tl, nInvoke, params, results)
		} else {
			fn = hostImpls[name]
		}
		if fn == nil {
			fn = unimplemented(name)
		}
		hb.NewFunctionBuilder().WithGoModuleFunction(api.GoModuleFunc(fn), params, results).WithName(name).Export(key)
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return i18n.Errorf(i18n.Msg{ES: "scadwasm: importaciones sin nombre conocido: %v", EN: "scadwasm: imports with no known name: %v"}, missing)
	}
	_, err = hb.Instantiate(ctx)
	return err
}

// unimplementedImports lista las importaciones que el host no implementa (las
// registra como un abort claro); un test exige que esté vacía.
func unimplementedImports(cm wazero.CompiledModule) []string {
	var out []string
	for _, def := range cm.ImportedFunctions() {
		_, key, _ := def.Import()
		name, ok := importNames[key]
		if !ok {
			out = append(out, key)
			continue
		}
		if strings.HasPrefix(name, "invoke_") || tableImpls[name] != nil {
			continue
		}
		if hostImpls[name] == nil {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

func unimplemented(name string) hostFn {
	return func(ctx context.Context, _ api.Module, _ []uint64) {
		procOf(ctx).die(&abortSignal{msg: "función del host sin implementar: " + name})
	}
}

// run arranca la instancia y ejecuta main. Devuelve el código de salida.
func (p *proc) run(ctx context.Context, mod api.Module) (int, error) {
	p.mod = mod
	p.mem = mod.Memory()
	p.nctx = context.WithoutCancel(ctx)
	p.start = time.Now()
	p.fnPool = map[uint64][]tableFn{}
	get := func(name string) api.Function { return mod.ExportedFunction(name) }
	p.fnSetThrew, p.fnStackSave, p.fnStackRestore = get(expSetThrew), get(expStackGet), get(expStackRestore)
	p.fnStackAlloc, p.fnTempRetSet, p.fnCanCatch = get(expStackAlloc), get(expTempRetSet), get(expCanCatch)
	p.fnIncRef, p.fnDecRef, p.fnGetExcPtr, p.fnMemalign = get(expIncRef), get(expDecRef), get(expGetExcPtr), get(expMemalign)
	for _, f := range []api.Function{p.fnSetThrew, p.fnStackSave, p.fnStackRestore, p.fnStackAlloc, p.fnTempRetSet,
		p.fnCanCatch, p.fnIncRef, p.fnDecRef, p.fnGetExcPtr, p.fnMemalign} {
		if f == nil || p.mem == nil {
			return 0, i18n.New(i18n.Msg{ES: "el wasm no exporta lo que el host espera: ¿es otra versión?", EN: "the wasm does not export what the host expects: is it another version?"})
		}
	}

	// Constructores estáticos de C++ (initRuntime del glue).
	if _, err := mod.ExportedFunction(expCtors).Call(ctx); err != nil {
		return 0, err
	}

	// callMain: argv en la pila del módulo.
	args := append([]string{"openscad"}, p.args...)
	argv, err := p.stackAllocU32(ctx, uint32((len(args)+1)*4))
	if err != nil {
		return 0, err
	}
	for i, a := range args {
		ptr, err := p.stackAllocString(ctx, a)
		if err != nil {
			return 0, err
		}
		p.mem.WriteUint32Le(argv+uint32(i*4), ptr)
	}
	p.mem.WriteUint32Le(argv+uint32(len(args)*4), 0)

	res, err := mod.ExportedFunction(expMain).Call(ctx, uint64(len(args)), uint64(argv))
	code := 0
	if err == nil && len(res) > 0 {
		code = int(api.DecodeI32(res[0]))
	}
	if err != nil {
		if es, ok := p.fatal.(*exitSignal); ok {
			return int(es.code), nil
		}
		return 0, p.runError(err)
	}
	// exitRuntime del glue: destructores globales y vaciado de stdio.
	if _, err := mod.ExportedFunction(expFuncsOnExit).Call(ctx); err != nil {
		if es, ok := p.fatal.(*exitSignal); ok {
			return int(es.code), nil
		}
		return code, nil // la salida ya está: no vale la pena fallar por esto
	}
	_, _ = mod.ExportedFunction(expFflush).Call(ctx, 0)
	return code, nil
}

// runError deja un error de ejecución legible: sin el rastro de pila de wazero
// (miles de líneas con una recursión profunda) y con la causa probable cuando
// el módulo se corrompe.
func (p *proc) runError(err error) error {
	if ab, ok := p.fatal.(*abortSignal); ok {
		return ab
	}
	msg := err.Error()
	if i := strings.Index(msg, "\nwasm stack trace"); i >= 0 {
		msg = msg[:i]
	}
	msg = strings.TrimSuffix(msg, " (recovered by wazero)")
	if p.throwing {
		msg = "excepción de C++ sin capturar"
	}
	if p.throwing || strings.Contains(msg, "out of bounds") || strings.Contains(msg, "unreachable") {
		return i18n.Errorf(i18n.Msg{
			ES: "%s. La pila del motor integrado es de solo 64 KB: una recursión muy profunda de módulos o funciones la desborda. Instala OpenSCAD para un modelo así",
			EN: "%s. The built-in engine has a 64 KB stack: very deep recursion of modules or functions overflows it. Install OpenSCAD for a model like this",
		}, msg)
	}
	return errors.New(msg)
}

// --- utilidades de memoria ------------------------------------------------------

func (p *proc) stackAllocU32(ctx context.Context, size uint32) (uint32, error) {
	p.scratch[0] = uint64(size)
	if err := p.fnStackAlloc.CallWithStack(ctx, p.scratch[:1]); err != nil {
		return 0, err
	}
	return uint32(p.scratch[0]), nil
}

func (p *proc) stackAllocString(ctx context.Context, s string) (uint32, error) {
	ptr, err := p.stackAllocU32(ctx, uint32(len(s)+1))
	if err != nil {
		return 0, err
	}
	b := append([]byte(s), 0)
	if !p.mem.Write(ptr, b) {
		return 0, i18n.New(i18n.Msg{ES: "memoria fuera de rango", EN: "memory out of range"})
	}
	return ptr, nil
}

// cstr lee una cadena terminada en NUL.
func (p *proc) cstr(ptr uint32) (string, bool) {
	if ptr == 0 {
		return "", true
	}
	const maxPath = 1 << 16
	var out []byte
	for off := uint32(0); off < maxPath; {
		chunk := uint32(256)
		size := p.mem.Size()
		if ptr+off >= size {
			return "", false
		}
		if ptr+off+chunk > size {
			chunk = size - (ptr + off)
		}
		b, ok := p.mem.Read(ptr+off, chunk)
		if !ok {
			return "", false
		}
		if i := indexByte(b, 0); i >= 0 {
			out = append(out, b[:i]...)
			return string(out), true
		}
		out = append(out, b...)
		off += chunk
	}
	return "", false
}

func indexByte(b []byte, c byte) int {
	for i, x := range b {
		if x == c {
			return i
		}
	}
	return -1
}

func (p *proc) u32(addr uint32) uint32 {
	v, _ := p.mem.ReadUint32Le(addr)
	return v
}

func (p *proc) put32(addr uint32, v uint32) bool { return p.mem.WriteUint32Le(addr, v) }
func (p *proc) put64(addr uint32, v uint64) bool { return p.mem.WriteUint64Le(addr, v) }
func (p *proc) put16(addr uint32, v uint16) bool { return p.mem.WriteUint16Le(addr, v) }
func (p *proc) put8(addr uint32, v byte) bool    { return p.mem.WriteByte(addr, v) }
