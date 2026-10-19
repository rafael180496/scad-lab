package llm

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"

	"scadlab/backend/i18n"
)

// requester lleva lo que todo request a un servidor necesita —URL base, token
// y el http.Client— y lo ejecuta traduciendo los fallos a mensajes útiles. Los
// tres clientes lo embeben, así que `c.base`, `c.token` y `c.http` se escriben
// igual en todos.
//
// A diferencia de mini-tools no hay cabeceras propias ni «aceptar certificado
// autofirmado»: ScadLab no tiene todavía dónde pedirlos, y un TLS sin verificar
// es una puerta que no se abre sin una pantalla que la explique.
type requester struct {
	base  string
	token string
	http  *http.Client
}

// newHTTPClient arma el cliente HTTP. Sin Client.Timeout a propósito: cortaría
// el stream a mitad de una respuesta larga. Los plazos los pone el contexto.
func newHTTPClient() *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.DialContext = (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	return &http.Client{Transport: tr}
}

// do ejecuta el request y traduce los fallos a mensajes útiles. Un status
// distinto de 2xx se devuelve como error con el cuerpo recortado. El token
// nunca aparece en un error.
func (r *requester) do(req *http.Request) (*http.Response, error) {
	if r.token != "" {
		req.Header.Set("Authorization", "Bearer "+r.token)
	}
	resp, err := r.http.Do(req)
	if err != nil {
		return nil, connError(req.Context(), r.base, err)
	}
	if resp.StatusCode/100 != 2 {
		defer resp.Body.Close()
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, &StatusError{Base: r.base, Status: resp.StatusCode, Msg: errorSnippet(body)}
	}
	return resp, nil
}

// ConnKind es la causa de un fallo a nivel de conexión.
type ConnKind int

const (
	// ConnOther: fallo de red sin una categoría propia.
	ConnOther ConnKind = iota
	// ConnRefused: nadie escucha en ese host:puerto.
	ConnRefused
	// ConnTimeout: se agotó el plazo (conectando o esperando).
	ConnTimeout
	// ConnTLS: el saludo TLS falló (certificado no confiable, servidor que
	// habla HTTP plano en un puerto https…).
	ConnTLS
	// ConnDNS: el nombre del host no se resuelve.
	ConnDNS
	// ConnReset: la conexión se cortó (reset, EOF, pipe rota).
	ConnReset
	// ConnDial: otro fallo al conectar (host o red inalcanzable…).
	ConnDial
	// ConnInterrupted: la respuesta se cortó leyendo el stream.
	ConnInterrupted
	// ConnEnded: el stream terminó sin completarse.
	ConnEnded
)

// ConnError es un fallo de conexión con su causa clasificada. Es un tipo
// propio para que quien llama decida sin parsear el texto: el chequeo previo
// del servidor muestra la causa y el runner reintenta una vez si es
// transitoria (IsRetryable).
type ConnError struct {
	Base string
	Kind ConnKind
	Err  error
}

func (e *ConnError) Unwrap() error { return e.Err }

func (e *ConnError) Error() string {
	switch e.Kind {
	case ConnRefused:
		return i18n.T(i18n.Msg{ES: "no se pudo conectar a %s: la conexión fue rechazada (¿está el servidor en marcha?)", EN: "could not connect to %s: connection refused (is the server running?)"}, e.Base)
	case ConnTimeout:
		return i18n.T(i18n.Msg{ES: "se agotó el tiempo de espera de %s", EN: "timed out waiting for %s"}, e.Base)
	case ConnTLS:
		if isCertError(e.Err) {
			return i18n.T(i18n.Msg{
				ES: "el certificado de %s no es de confianza (%v)",
				EN: "the certificate of %s is not trusted (%v)",
			}, e.Base, e.Err)
		}
		return i18n.T(i18n.Msg{ES: "falló la conexión segura (TLS) con %s: %v", EN: "the secure (TLS) connection to %s failed: %v"}, e.Base, e.Err)
	case ConnDNS:
		return i18n.T(i18n.Msg{ES: "no se pudo conectar a %s: no se encontró el nombre del servidor (%v)", EN: "could not connect to %s: the server name was not found (%v)"}, e.Base, e.Err)
	case ConnReset:
		return i18n.T(i18n.Msg{ES: "se perdió la conexión con %s: %v", EN: "the connection to %s was lost: %v"}, e.Base, e.Err)
	case ConnInterrupted:
		return i18n.T(i18n.Msg{ES: "se cortó la respuesta de %s: %v", EN: "the response from %s was interrupted: %v"}, e.Base, e.Err)
	case ConnEnded:
		return i18n.T(i18n.Msg{ES: "la respuesta de %s terminó sin completarse", EN: "the response from %s ended before completing"}, e.Base)
	}
	return i18n.T(i18n.Msg{ES: "no se pudo conectar a %s: %v", EN: "could not connect to %s: %v"}, e.Base, e.Err)
}

// Retryable dice si el fallo es del tipo que un segundo intento puede curar:
// el servidor se reiniciaba, la conexión se cayó, un hipo de red.
func (e *ConnError) Retryable() bool {
	switch e.Kind {
	case ConnRefused, ConnTimeout, ConnReset, ConnDial, ConnEnded:
		return true
	case ConnInterrupted:
		return isResetLike(e.Err)
	}
	return false
}

// IsRetryable dice si err es un fallo de conexión transitorio (ver
// ConnError.Retryable). Un 4xx/5xx (StatusError), un cancelado y todo lo que
// no sea de conexión dan false.
func IsRetryable(err error) bool {
	var ce *ConnError
	return errors.As(err, &ce) && ce.Retryable()
}

// IsTLSError dice si err es un fallo del saludo TLS.
func IsTLSError(err error) bool {
	var ce *ConnError
	return errors.As(err, &ce) && ce.Kind == ConnTLS
}

// connError distingue las causas de un fallo de red que cambian qué hacer.
func connError(ctx context.Context, base string, err error) error {
	// Cancelado por quien llama: se devuelve tal cual para que lo reconozca.
	if errors.Is(ctx.Err(), context.Canceled) {
		return context.Canceled
	}
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	ce := &ConnError{Base: base, Err: err}
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded) || isTimeout(err):
		ce.Kind = ConnTimeout
	case errors.Is(err, syscall.ECONNREFUSED):
		ce.Kind = ConnRefused
	case isTLSFailure(err):
		ce.Kind = ConnTLS
	case isDNSFailure(err):
		ce.Kind = ConnDNS
	case isResetLike(err):
		ce.Kind = ConnReset
	case isDialFailure(err):
		ce.Kind = ConnDial
	default:
		ce.Kind = ConnOther
	}
	return ce
}

// interruptedError es el corte de un stream ya abierto.
func interruptedError(base string, err error) error {
	return &ConnError{Base: base, Kind: ConnInterrupted, Err: err}
}

// endedError es un stream que se cerró sin llegar al final.
func endedError(base string) error {
	return &ConnError{Base: base, Kind: ConnEnded, Err: io.ErrUnexpectedEOF}
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}

func isCertError(err error) bool {
	var ua x509.UnknownAuthorityError
	var ci x509.CertificateInvalidError
	var he x509.HostnameError
	var cv *tls.CertificateVerificationError
	return errors.As(err, &ua) || errors.As(err, &ci) || errors.As(err, &he) || errors.As(err, &cv)
}

func isTLSFailure(err error) bool {
	if isCertError(err) {
		return true
	}
	var rh tls.RecordHeaderError
	if errors.As(err, &rh) {
		return true
	}
	// Las alertas TLS (tls.alert) no tienen tipo exportado: se reconocen por
	// el texto que arma crypto/tls.
	msg := err.Error()
	return strings.HasPrefix(msg, "tls:") || strings.Contains(msg, "remote error: tls")
}

func isDNSFailure(err error) bool {
	var de *net.DNSError
	return errors.As(err, &de)
}

func isResetLike(err error) bool {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EPIPE) || errors.Is(err, syscall.ECONNABORTED) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "connection reset") || strings.Contains(msg, "broken pipe") || strings.HasSuffix(msg, "EOF")
}

func isDialFailure(err error) bool {
	var oe *net.OpError
	return errors.As(err, &oe) && oe.Op == "dial"
}
