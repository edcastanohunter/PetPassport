package httpserver

import (
	"bytes"
	"context"
	"io"
	"net/http"

	"github.com/edcastanohunter/petpassport/backend/internal/platform/problem"
)

type rawBodyKey struct{}

// captureBody lee el cuerpo (ya acotado por RequestSize) y lo deja disponible
// en el contexto para handlers que necesitan distinguir `null` de ausente
// (JSON Merge Patch). El cuerpo se restaura para el decodificador generado.
func captureBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Body == nil || r.Body == http.NoBody || r.Method == http.MethodGet || r.Method == http.MethodHead {
			next.ServeHTTP(w, r)
			return
		}
		b, err := io.ReadAll(r.Body)
		_ = r.Body.Close()
		if err != nil {
			// RequestSize devuelve error al superar el límite.
			problem.Write(w, r, problem.New(problem.PayloadTooLarge, "Cuerpo demasiado grande", "El cuerpo supera el límite permitido."))
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(b))
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), rawBodyKey{}, b)))
	})
}

// RawBodyFrom devuelve el cuerpo crudo capturado (nil si no hay).
func RawBodyFrom(ctx context.Context) []byte {
	b, _ := ctx.Value(rawBodyKey{}).([]byte)
	return b
}
