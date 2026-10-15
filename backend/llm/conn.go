package llm

import (
	"context"
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

// requester lleva lo que todo request necesita —URL base, token y el
// http.Client— y lo ejecuta traduciendo los fallos a mensajes útiles.
type requester struct {
	base  string
	token string
	http  *http.Client
}

// newHTTPClient arma el cliente HTTP. Sin Client.Timeout a propósito: cortaría
// el stream a mitad de una respuesta larga; los plazos los pone el contexto.
func newHTTPClient() *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.DialContext = (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	return &http.Client{Transport: tr}
}

// do ejecuta el request. Un status distinto de 2xx se devuelve como error con
// el cuerpo recortado. El token nunca aparece en un error.
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

// maxErrBody es cuánto del cuerpo de un error se incluye en el mensaje.
const maxErrBody = 300

// StatusError es una respuesta HTTP que no fue 2xx. Es un tipo propio para que
// quien llama pueda mirar el status (un 401 invita a revisar la clave) sin
// parsear el texto.
type StatusError struct {
	Base   string
	Status int
	Msg    string
}

func (e *StatusError) Error() string {
	return i18n.T(i18n.Msg{ES: "el servidor %s respondió %d: %s", EN: "the server %s answered %d: %s"}, e.Base, e.Status, e.Msg)
}

// ConnKind es la causa de un fallo a nivel de conexión.
type ConnKind int

const (
	ConnOther       ConnKind = iota // fallo de red sin categoría propia
	ConnRefused                     // nadie escucha en ese host:puerto
	ConnTimeout                     // se agotó el plazo
	ConnDNS                         // el nombre del host no se resuelve
	ConnInterrupted                 // la respuesta se cortó leyendo el stream
	ConnEnded                       // el stream terminó sin completarse
)

// ConnError es un fallo de conexión con su causa clasificada, para mostrar
// "¿está el servidor en marcha?" y no un "dial tcp: connect: connection refused".
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
	case ConnDNS:
		return i18n.T(i18n.Msg{ES: "no se pudo conectar a %s: no se encontró el nombre del servidor (%v)", EN: "could not connect to %s: the server name was not found (%v)"}, e.Base, e.Err)
	case ConnInterrupted:
		return i18n.T(i18n.Msg{ES: "se cortó la respuesta de %s: %v", EN: "the response from %s was interrupted: %v"}, e.Base, e.Err)
	case ConnEnded:
		return i18n.T(i18n.Msg{ES: "la respuesta de %s terminó sin completarse", EN: "the response from %s ended before completing"}, e.Base)
	}
	return i18n.T(i18n.Msg{ES: "no se pudo conectar a %s: %v", EN: "could not connect to %s: %v"}, e.Base, e.Err)
}

// connError clasifica un fallo de red. Un contexto cancelado por quien llama
// se devuelve tal cual (context.Canceled) para que lo reconozca.
func connError(ctx context.Context, base string, err error) error {
	if errors.Is(ctx.Err(), context.Canceled) {
		return context.Canceled
	}
	var ue *url.Error
	if errors.As(err, &ue) {
		err = ue.Err
	}
	ce := &ConnError{Base: base, Err: err}
	var de *net.DNSError
	var ne net.Error
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()):
		ce.Kind = ConnTimeout
	case errors.Is(err, syscall.ECONNREFUSED):
		ce.Kind = ConnRefused
	case errors.As(err, &de):
		ce.Kind = ConnDNS
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

// errorSnippet saca el mensaje de un cuerpo de error. Ollama responde
// `{"error":"texto"}` y OpenAI `{"error":{"message":"…"}}`; si no es ninguno se
// muestra el comienzo del cuerpo crudo.
func errorSnippet(body []byte) string { return trimSnippet(errorFromBody(body), body) }

func trimSnippet(msg string, body []byte) string {
	if msg == "" {
		msg = strings.TrimSpace(string(body))
	}
	if len(msg) > maxErrBody {
		msg = msg[:maxErrBody] + "…"
	}
	if msg == "" {
		return "-"
	}
	return msg
}
