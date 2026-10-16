package scadwasm

import (
	"context"
	"fmt"
	"strings"

	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/experimental/table"
)

// Excepciones de C++ y setjmp/longjmp, al estilo de emscripten con
// -fexceptions: el módulo no usa las instrucciones de excepción de WebAssembly
// sino que cada llamada que puede lanzar va envuelta en un invoke_*, y lanzar
// es un «throw» del host. Aquí el throw es un panic(throwSignal{}) que el
// invoke_* que lo rodea atrapa; el estado de la excepción (puntero, pila de
// capturadas) vive en proc, como en el glue de JavaScript.
//
// Layout de la cabecera de una excepción de C++ (wasm32), a excPtr-24:
//
//	+4 tipo  +8 destructor  +12 capturada (byte)  +13 relanzada (byte)  +16 ptr ajustado

const excHeader = 24

func (p *proc) setCaught(excPtr uint32, v bool) {
	b := byte(0)
	if v {
		b = 1
	}
	p.put8(excPtr-excHeader+12, b)
}

func (p *proc) isCaught(excPtr uint32) bool {
	b, _ := p.mem.ReadByte(excPtr - excHeader + 12)
	return b != 0
}

func (p *proc) setRethrown(excPtr uint32, v bool) {
	b := byte(0)
	if v {
		b = 1
	}
	p.put8(excPtr-excHeader+13, b)
}

func (p *proc) isRethrown(excPtr uint32) bool {
	b, _ := p.mem.ReadByte(excPtr - excHeader + 13)
	return b != 0
}

// call llama a un export de pocos parámetros sin asignar memoria.
func (p *proc) call(fn api.Function, args ...uint64) uint64 {
	p.scratch[0] = 0
	copy(p.scratch[:], args)
	// p.nctx y no el contexto de la llamada: ver proc.nctx.
	if err := fn.CallWithStack(p.nctx, p.scratch[:max(len(args), 1)]); err != nil {
		panic(err)
	}
	return p.scratch[0]
}

// findMatchingCatch decide a qué cláusula catch va una excepción (el
// findMatchingCatch del glue).
func (p *proc) findMatchingCatch(types ...uint32) uint32 {
	thrown := p.exceptionLast
	if thrown == 0 {
		p.call(p.fnTempRetSet, 0)
		return 0
	}
	p.put32(thrown-excHeader+16, thrown) // puntero ajustado
	thrownType := p.u32(thrown - excHeader + 4)
	if thrownType == 0 {
		p.call(p.fnTempRetSet, 0)
		return thrown
	}
	for _, caughtType := range types {
		if caughtType == 0 || caughtType == thrownType {
			break
		}
		adjustedAddr := thrown - excHeader + 16
		if uint32(p.call(p.fnCanCatch, uint64(caughtType), uint64(thrownType), uint64(adjustedAddr))) != 0 {
			p.call(p.fnTempRetSet, uint64(caughtType))
			return thrown
		}
	}
	p.call(p.fnTempRetSet, uint64(thrownType))
	return thrown
}

func init() {
	hostImpls["___cxa_throw"] = func(ctx context.Context, _ api.Module, st []uint64) {
		p := procOf(ctx)
		ptr, typ, dtor := uint32(st[0]), uint32(st[1]), uint32(st[2])
		// info.init(type, destructor)
		p.put32(ptr-excHeader+16, 0)
		p.put32(ptr-excHeader+4, typ)
		p.put32(ptr-excHeader+8, dtor)
		p.exceptionLast = ptr
		p.uncaught++
		p.throw()
	}
	hostImpls["___cxa_rethrow"] = func(ctx context.Context, _ api.Module, st []uint64) {
		p := procOf(ctx)
		if len(p.caught) == 0 {
			p.die(&abortSignal{msg: "no exception to throw"})
		}
		ptr := p.caught[len(p.caught)-1]
		p.caught = p.caught[:len(p.caught)-1]
		if !p.isRethrown(ptr) {
			p.caught = append(p.caught, ptr)
			p.setRethrown(ptr, true)
			p.setCaught(ptr, false)
			p.uncaught++
		}
		p.exceptionLast = ptr
		p.throw()
	}
	hostImpls["___resumeException"] = func(ctx context.Context, _ api.Module, st []uint64) {
		p := procOf(ctx)
		if p.exceptionLast == 0 {
			p.exceptionLast = uint32(st[0])
		}
		p.throw()
	}
	hostImpls["__emscripten_throw_longjmp"] = func(ctx context.Context, _ api.Module, st []uint64) {
		procOf(ctx).throw()
	}
	hostImpls["___cxa_begin_catch"] = func(ctx context.Context, _ api.Module, st []uint64) {
		p := procOf(ctx)
		ptr := uint32(st[0])
		if !p.isCaught(ptr) {
			p.setCaught(ptr, true)
			p.uncaught--
		}
		p.setRethrown(ptr, false)
		p.caught = append(p.caught, ptr)
		p.call(p.fnIncRef, uint64(ptr))
		st[0] = p.call(p.fnGetExcPtr, uint64(ptr))
	}
	hostImpls["___cxa_end_catch"] = func(ctx context.Context, _ api.Module, st []uint64) {
		p := procOf(ctx)
		p.call(p.fnSetThrew, 0, 0)
		if len(p.caught) == 0 {
			p.die(&abortSignal{msg: "__cxa_end_catch sin excepción capturada"})
		}
		ptr := p.caught[len(p.caught)-1]
		p.caught = p.caught[:len(p.caught)-1]
		p.call(p.fnDecRef, uint64(ptr))
		p.exceptionLast = 0
	}
	hostImpls["___cxa_current_primary_exception"] = func(ctx context.Context, _ api.Module, st []uint64) {
		p := procOf(ctx)
		if len(p.caught) == 0 {
			st[0] = 0
			return
		}
		ptr := p.caught[len(p.caught)-1]
		p.call(p.fnIncRef, uint64(ptr))
		st[0] = uint64(ptr)
	}
	hostImpls["___cxa_uncaught_exceptions"] = func(ctx context.Context, _ api.Module, st []uint64) {
		st[0] = api.EncodeI32(procOf(ctx).uncaught)
	}
	hostImpls["_llvm_eh_typeid_for"] = func(_ context.Context, _ api.Module, st []uint64) {
		st[0] = uint64(uint32(st[0]))
	}
	for i, name := range []string{"___cxa_find_matching_catch_2", "___cxa_find_matching_catch_3", "___cxa_find_matching_catch_4", "___cxa_find_matching_catch_5"} {
		n := i // número de tipos: 0..3
		hostImpls[name] = func(ctx context.Context, _ api.Module, st []uint64) {
			p := procOf(ctx)
			types := make([]uint32, n)
			for j := range types {
				types[j] = uint32(st[j])
			}
			st[0] = uint64(p.findMatchingCatch(types...))
		}
	}
}

// tableFn es una función de la tabla indirecta lista para llamar con la pila
// de parámetros y resultados.
type tableFn func(ctx context.Context, stack []uint64) error

// tableLookup resuelve posiciones de la tabla indirecta: funciones del propio
// módulo (por wazero) o importaciones del host (ver hostSlots).
type tableLookup struct {
	host map[uint32]hostFn // posición → función del host
}

func (t *tableLookup) lookup(mod api.Module, idx uint32, params, results []api.ValueType) tableFn {
	if hf, ok := t.host[idx]; ok {
		return func(ctx context.Context, stack []uint64) (err error) {
			defer func() {
				if r := recover(); r != nil {
					if e, ok := r.(error); ok {
						err = e
						return
					}
					err = fmt.Errorf("%v", r)
				}
			}()
			hf(ctx, mod, stack)
			return nil
		}
	}
	// Es el call_indirect del host: un índice nulo o de otro tipo es un trap,
	// igual que en WebAssembly.
	fn := table.LookupFunction(mod, 0, idx, params, results)
	return func(ctx context.Context, stack []uint64) error { return fn.CallWithStack(ctx, stack) }
}

// makeInvoke genera un invoke_<firma>(índice, args…): llama a la función de la
// tabla indirecta y, si lanza una excepción (o un longjmp), restaura la pila
// del módulo y marca __THREW__ con setThrew para que el código que sigue a la
// llamada vea que hubo una excepción y salte a su cláusula catch o cleanup.
// Cualquier otra cosa —exit(), abort(), una cancelación, un trap— sigue
// propagándose.
func makeInvoke(tl *tableLookup, sigNum uint32, params, results []api.ValueType) hostFn {
	callParams := params[1:]
	nres := len(results)
	nbuf := max(len(callParams), nres, 1)
	return func(ctx context.Context, mod api.Module, st []uint64) {
		p := procOf(ctx)
		idx := uint32(st[0])
		sp := uint32(p.call(p.fnStackSave))

		key := uint64(idx) | uint64(sigNum)<<32
		var fn tableFn
		if pool := p.fnPool[key]; len(pool) > 0 {
			fn = pool[len(pool)-1]
			p.fnPool[key] = pool[:len(pool)-1]
		} else {
			fn = tl.lookup(mod, idx, callParams, results)
		}
		buf := make([]uint64, nbuf)
		copy(buf, st[1:1+len(callParams)])
		err := fn(p.nctx, buf)
		if err == nil {
			p.fnPool[key] = append(p.fnPool[key], fn)
			if nres > 0 {
				st[0] = buf[0]
			}
			return
		}
		// Solo un throw de C++ o un longjmp lo atrapa el invoke; exit(), abort(),
		// una cancelación o un trap siguen su camino. Se decide con las banderas
		// de proc y no con errors.As: con miles de invoke anidados (una
		// recursión profunda) recorrer la cadena de errores en cada nivel era
		// cuadrático.
		if p.fatal != nil || !p.throwing {
			panic(err)
		}
		p.throwing = false
		// El camino de la excepción: la función se descarta (su pila quedó en
		// un estado de error).
		p.call(p.fnStackRestore, uint64(sp))
		p.call(p.fnSetThrew, 1, 0)
		if nres > 0 {
			st[0] = 0
		}
	}
}

func init() {
	hostImpls["___assert_fail"] = func(ctx context.Context, _ api.Module, st []uint64) {
		p := procOf(ctx)
		cond, _ := p.cstr(uint32(st[0]))
		file, _ := p.cstr(uint32(st[1]))
		fn, _ := p.cstr(uint32(st[3]))
		if file == "" {
			file = "unknown filename"
		}
		if fn == "" {
			fn = "unknown function"
		}
		msg := "Assertion failed: " + cond + ", at: " + strings.Join([]string{file, itoa(int(int32(st[2]))), fn}, ",")
		p.out.Write([]byte(msg + "\n"))
		p.die(&abortSignal{msg: msg})
	}
	hostImpls["__abort_js"] = func(ctx context.Context, _ api.Module, _ []uint64) {
		procOf(ctx).die(&abortSignal{})
	}
	hostImpls["_exit"] = func(ctx context.Context, _ api.Module, st []uint64) {
		procOf(ctx).die(&exitSignal{code: int32(uint32(st[0]))})
	}
	hostImpls["_proc_exit"] = hostImpls["_exit"]
	hostImpls["__emscripten_runtime_keepalive_clear"] = func(context.Context, api.Module, []uint64) {}
}
